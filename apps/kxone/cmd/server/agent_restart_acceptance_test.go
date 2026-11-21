package main

// This fixture is a separate, opt-in process. It never uses the production DB
// as business data. Real mode reads approved model settings and uses only the
// existing restricted shared-ledger connection. Starting it makes no model call.
//
// KXONE_AGENT_RESTART_ACCEPTANCE=1
// KXONE_AGENT_RESTART_MODE=live (or controlled for local protocol tests)
// KXONE_AGENT_RESTART_PHASE=init|resume
// KXONE_AGENT_RESTART_DIR=/tmp/crm-agent-restart-<new-private-name>
// KXONE_AGENT_RESTART_LISTEN=127.0.0.1:0
// Live mode also uses KXONE_AGENT_LIVE_SOURCE_DB and its existing environment
// encryption key. Kill only the PID in fixture-access.json, then resume using
// the same test binary and directory. Never reuse this entry point for production.
import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lsaac128/kxone/internal/agent"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type agentRestartFixture struct {
	directory string
	database  string
	fieldKey  string
	manifest  restartManifest
}

type restartManifest struct {
	Format          string `json:"format"`
	Mode            string `json:"mode"`
	FixtureSHA      string `json:"fixture_sha256"`
	BinarySHA       string `json:"test_binary_sha256"`
	ConfigSHA       string `json:"configuration_sha256"`
	SourceSHA       string `json:"ledger_source_identity_sha256"`
	AccountID       int64  `json:"account_id"`
	SettingsVersion int64  `json:"settings_version"`
	RuleVersion     int64  `json:"rule_version"`
	Model           string `json:"model"`
	Ready           bool   `json:"ready"`
	BusinessSHA     string `json:"business_sha256"`
}

func restartDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func restartPrivateFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return errors.New("restart evidence must be a regular private 0600 file")
	}
	return nil
}

func restartWriteJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".restart-evidence-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if _, err = file.Write(append(raw, '\n')); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(temporary, path)
	}
	return err
}

// Restrict the persistent fixture to a newly created named temporary directory.
// Init never opens an existing path. Resume rejects symlinks, non-private files,
// incompatible identities and non-gold business data before opening a writer.
func prepareAgentRestartFixture(t *testing.T, path, phase string, expected restartManifest) *agentRestartFixture {
	t.Helper()
	if !filepath.IsAbs(path) || !strings.HasPrefix(filepath.Base(path), "crm-agent-restart-") {
		t.Fatal("restart directory must be an absolute, new crm-agent-restart-* directory under the system temporary root")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		t.Fatal("restart parent unavailable")
	}
	temporary, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(temporary, parent)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		t.Fatal("persistent restart fixture must remain under the system temporary root")
	}
	path = filepath.Join(parent, filepath.Base(path))
	if phase == "init" {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal("restart init requires a previously nonexistent directory")
		}
	} else if phase != "resume" {
		t.Fatal("restart phase must be init or resume")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0700 {
		t.Fatal("restart directory must be a real private 0700 directory")
	}
	lockPath := filepath.Join(path, "process.lock")
	if phase == "resume" {
		if err := restartPrivateFile(lockPath); err != nil {
			t.Fatal(err)
		}
	}
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Close() })
	if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		t.Fatal("restart fixture already belongs to a running process")
	}
	fixture := &agentRestartFixture{directory: path, database: filepath.Join(path, "kxone-test.db"), manifest: expected}
	manifestPath, keyPath := filepath.Join(path, "identity.json"), filepath.Join(path, "field-key")
	if phase == "init" {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			t.Fatal(err)
		}
		fixture.fieldKey = base64.StdEncoding.EncodeToString(key)
		clear(key)
		if err := os.WriteFile(keyPath, []byte(fixture.fieldKey), 0600); err != nil {
			t.Fatal("restart key could not be saved privately")
		}
		if err := restartWriteJSON(manifestPath, expected); err != nil {
			t.Fatal(err)
		}
	} else {
		for _, file := range []string{manifestPath, keyPath, fixture.database} {
			if err := restartPrivateFile(file); err != nil {
				t.Fatal(err)
			}
		}
		raw, err := os.ReadFile(manifestPath)
		if err != nil || json.Unmarshal(raw, &fixture.manifest) != nil || !fixture.manifest.Ready || fixture.manifest.BusinessSHA == "" {
			t.Fatal("restart fixture initialization was incomplete")
		}
		identity := fixture.manifest
		identity.Ready, identity.BusinessSHA = false, ""
		if identity != expected {
			t.Fatal("restart fixture binary, data, account, rules or model configuration identity changed")
		}
		key, err := os.ReadFile(keyPath)
		if err != nil {
			t.Fatal("restart private key unavailable")
		}
		decoded, err := base64.StdEncoding.DecodeString(string(key))
		if err != nil || len(decoded) != 32 {
			t.Fatal("restart private key invalid")
		}
		clear(decoded)
		fixture.fieldKey = string(key)
		clear(key)
		readOnly := restartReadOnlyDB(t, fixture.database)
		business, err := captureLiveBusinessFingerprint(t.Context(), readOnly)
		if err != nil || business.SHA256 != fixture.manifest.BusinessSHA {
			t.Fatal("restart business identity differs from the first imported gold fixture")
		}
		var ruleVersion int64
		if err := readOnly.Model(&agent.Rule{}).Where("status = ?", "published").Select("COALESCE(MAX(version), 0)").Scan(&ruleVersion).Error; err != nil || ruleVersion != expected.RuleVersion {
			t.Fatal("restart public rules changed")
		}
	}
	return fixture
}

func restartReadOnlyDB(t *testing.T, path string) *gorm.DB {
	t.Helper()
	uri := (&url.URL{Scheme: "file", Path: path}).String() + "?mode=ro&_query_only=1&_busy_timeout=5000"
	conn, err := sql.Open("sqlite3", uri)
	if err != nil {
		t.Fatal(err)
	}
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = conn.Close() })
	db, err := gorm.Open(sqlite.Dialector{Conn: conn}, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

type restartObservation struct {
	Runs         []agent.Run               `json:"runs"`
	Reservations []agent.BudgetReservation `json:"reservations"`
	Snapshots    int                       `json:"temporary_snapshots"`
	ResultSHA    map[string]string         `json:"result_sha256"`
	DoneEvents   map[string]int            `json:"done_events"`
	DeltaEvents  int                       `json:"delta_events"`
	BusinessSHA  string                    `json:"business_sha256"`
}

func observeRestartFixture(ctx context.Context, db, ledger *gorm.DB, directory string) (restartObservation, error) {
	result := restartObservation{Runs: []agent.Run{}, Reservations: []agent.BudgetReservation{}, ResultSHA: map[string]string{}, DoneEvents: map[string]int{}}
	if err := db.WithContext(ctx).Order("id").Find(&result.Runs).Error; err != nil {
		return result, err
	}
	ids := []string{}
	for _, run := range result.Runs {
		ids = append(ids, run.ID)
	}
	if len(ids) > 0 {
		if err := ledger.WithContext(ctx).Where("run_id IN ?", ids).Order("id").Find(&result.Reservations).Error; err != nil {
			return result, err
		}
	}
	var records []agent.ResultRecord
	if err := db.WithContext(ctx).Find(&records).Error; err != nil {
		return result, err
	}
	for _, record := range records {
		result.ResultSHA[record.ID] = restartDigest([]byte(record.Data))
	}
	var events []agent.Event
	if err := db.WithContext(ctx).Select("run_id,type").Where("type IN ?", []string{"done", "delta"}).Find(&events).Error; err != nil {
		return result, err
	}
	for _, event := range events {
		if event.Type == "done" {
			result.DoneEvents[event.RunID]++
		} else {
			result.DeltaEvents++
		}
	}
	files, err := os.ReadDir(directory)
	if err != nil {
		return result, err
	}
	for _, file := range files {
		if strings.HasPrefix(file.Name(), "snapshot-") {
			result.Snapshots++
		}
	}
	fingerprint, err := captureLiveBusinessFingerprint(ctx, db)
	result.BusinessSHA = fingerprint.SHA256
	return result, err
}

func TestAgentRestartAcceptance(t *testing.T) {
	if os.Getenv("KXONE_AGENT_RESTART_ACCEPTANCE") != "1" {
		t.Skip("explicit opt-in required; this entry point starts no model request")
	}
	mode, phase := os.Getenv("KXONE_AGENT_RESTART_MODE"), os.Getenv("KXONE_AGENT_RESTART_PHASE")
	listen := os.Getenv("KXONE_AGENT_RESTART_LISTEN")
	host, _, err := net.SplitHostPort(listen)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		t.Fatal("restart server requires an explicit loopback address")
	}
	gold, fixtureSHA := loadLiveFixture(t)
	cfg := agent.DefaultConfig()
	cfg.Enabled, cfg.APIKey, cfg.Model = true, "isolated-restart-model", "controlled-restart-model"
	actorID, settingsVersion := int64(9000000), int64(0)
	source := ""
	if mode == "live" {
		source = os.Getenv("KXONE_AGENT_LIVE_SOURCE_DB")
		if !filepath.IsAbs(source) {
			t.Fatal("live restart requires the approved absolute settings/ledger source")
		}
		cfg, actorID, settingsVersion = readLiveConfig(t, source)
	} else if mode == "controlled" {
		cfg.BaseURL = os.Getenv("KXONE_AGENT_RESTART_MODEL_URL")
		parsed, err := url.Parse(cfg.BaseURL)
		if err != nil || parsed.Scheme != "http" || parsed.User != nil || net.ParseIP(parsed.Hostname()) == nil || !net.ParseIP(parsed.Hostname()).IsLoopback() {
			t.Fatal("controlled restart model must be an explicit local HTTP fixture")
		}
	} else {
		t.Fatal("restart mode must be controlled or live")
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binaryFile, err := os.Open(binary)
	if err != nil {
		t.Fatal("test binary identity unavailable")
	}
	binaryHash := sha256.New()
	_, hashErr := io.Copy(binaryHash, binaryFile)
	_ = binaryFile.Close()
	if hashErr != nil {
		t.Fatal("test binary identity unreadable")
	}
	encodedConfig, err := json.Marshal(cfg) // Config's credentials and callbacks are json:"-".
	if err != nil {
		t.Fatal(err)
	}
	identity := restartManifest{Format: "agent-process-restart-v1", Mode: mode, FixtureSHA: fixtureSHA, BinarySHA: hex.EncodeToString(binaryHash.Sum(nil)), ConfigSHA: restartDigest(encodedConfig), SourceSHA: restartDigest([]byte(source)), AccountID: actorID, SettingsVersion: settingsVersion, RuleVersion: 0, Model: cfg.Model}
	private := prepareAgentRestartFixture(t, os.Getenv("KXONE_AGENT_RESTART_DIR"), phase, identity)
	cfg.Directory = filepath.Join(private.directory, "agent-runtime")
	var auditMu sync.Mutex
	cfg.RequestAudit = func(body []byte) error {
		auditMu.Lock()
		defer auditMu.Unlock()
		if mode == "live" {
			if err := checkLiveSourceUnchanged(source, actorID, settingsVersion); err != nil {
				return err
			}
		}
		for _, fragment := range gold.IndependentExpected["forbidden_provider_payload_fragments"].([]any) {
			if strings.Contains(string(body), fragment.(string)) {
				return errors.New("restart request privacy guard failed")
			}
		}
		if strings.Contains(string(body), cfg.APIKey) {
			return errors.New("restart request credential guard failed")
		}
		file, err := os.OpenFile(filepath.Join(private.directory, "outbound-audit.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return errors.New("restart metadata audit unavailable")
		}
		defer file.Close()
		// Before transmission is not evidence that the provider completed or billed
		// this call. After SIGKILL its unknown usage must remain reserved.
		err = json.NewEncoder(file).Encode(map[string]any{"at": time.Now().UTC(), "pid": os.Getpid(), "bytes": len(body), "request_sha256": restartDigest(body), "audit_stage": "before_transmission", "synthetic_business_only": true, "real_provider": mode == "live"})
		if err == nil {
			err = file.Sync()
		}
		return err
	}
	var db, ledger *gorm.DB
	serviceOptions := []agent.ServiceOption{}
	if mode == "live" {
		ledger = openLiveLedger(t, source)
		serviceOptions = append(serviceOptions, agent.WithBudget(agent.NewBudget(ledger)))
	}
	prepare := func(t *testing.T, fixtureDB *gorm.DB) {
		if phase == "init" {
			importLiveFixture(t, fixtureDB, gold, actorID)
		}
	}
	router := newTestRouterWithOptions(t, testRouterOptions{agentConfig: &cfg, agentPublicModel: mode == "live", agentFixtureDB: &db, agentServiceOptions: serviceOptions, agentPrepareFixture: prepare, agentRestartFixture: private})
	if mode != "live" {
		ledger = db
	}
	if err := os.Chmod(private.database, 0600); err != nil {
		t.Fatal(err)
	}
	token := issueDevToken(t, router, "live-evaluation-admin", "admin")
	business, err := captureLiveBusinessFingerprint(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	if phase == "init" {
		private.manifest.Ready, private.manifest.BusinessSHA = true, business.SHA256
		if err := restartWriteJSON(filepath.Join(private.directory, "identity.json"), private.manifest); err != nil {
			t.Fatal(err)
		}
	} else if private.manifest.BusinessSHA != business.SHA256 {
		t.Fatal("restart modified business data")
	}
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "private restart fixture", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/__restart_observation" {
			value, err := observeRestartFixture(r.Context(), db, ledger, cfg.Directory)
			if err != nil {
				http.Error(w, "restart evidence unavailable", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(value)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/agent/conversations") && !strings.HasPrefix(r.URL.Path, "/api/agent/runs/") && !strings.HasPrefix(r.URL.Path, "/api/agent/results/") && !strings.HasPrefix(r.URL.Path, "/api/agent/exports/") {
			http.Error(w, "restart fixture exposes only conversation analysis", http.StatusForbidden)
			return
		}
		router.ServeHTTP(w, r)
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	t.Cleanup(func() { _ = server.Close() })
	serve := make(chan error, 1)
	go func() { serve <- server.Serve(listener) }()
	access := map[string]any{"base_url": "http://" + listener.Addr().String(), "token": token, "pid": os.Getpid(), "phase": phase, "mode": mode, "fixture_hash": fixtureSHA, "test_binary_sha256": identity.BinarySHA, "model": cfg.Model, "account_id": actorID, "rule_version": identity.RuleVersion, "settings_version": settingsVersion, "real_model_acceptance_passed": false, "database": private.database, "observation_path": "/__restart_observation"}
	if err := restartWriteJSON(filepath.Join(private.directory, "fixture-access.json"), access); err != nil {
		t.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(t.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Hour):
	case err := <-serve:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Fatal(err)
		}
	}
}

type restartAccess struct {
	BaseURL  string `json:"base_url"`
	Token    string `json:"token"`
	PID      int    `json:"pid"`
	Database string `json:"database"`
}

func restartRequest(t *testing.T, access restartAccess, method, path string, body, destination any) {
	t.Helper()
	var payload bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&payload).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, access.BaseURL+path, &payload)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+access.Token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		t.Fatalf("restart fixture %s %s returned HTTP %d", method, path, response.StatusCode)
	}
	if destination != nil {
		if err := json.NewDecoder(response.Body).Decode(destination); err != nil {
			t.Fatal(err)
		}
	}
}

func restartChildCommand(t *testing.T, directory, phase, model string) *exec.Cmd {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestAgentRestartAcceptance$", "-test.timeout=120s")
	// No inherited source credentials or opt-in acceptance configuration can
	// turn the deterministic subprocess into a paid call.
	for _, entry := range os.Environ() {
		key := strings.SplitN(entry, "=", 2)[0]
		if !strings.HasPrefix(key, "KXONE_AGENT_") && !strings.Contains(key, "ENCRYPTION_KEY") && !strings.Contains(key, "API_KEY") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "KXONE_AGENT_RESTART_ACCEPTANCE=1", "KXONE_AGENT_RESTART_MODE=controlled", "KXONE_AGENT_RESTART_PHASE="+phase, "KXONE_AGENT_RESTART_DIR="+directory, "KXONE_AGENT_RESTART_LISTEN=127.0.0.1:0", "KXONE_AGENT_RESTART_MODEL_URL="+model)
	return cmd
}

func rejectRestartChild(t *testing.T, directory, phase, model, expected string) {
	t.Helper()
	cmd := restartChildCommand(t, directory, phase, model)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	completed := make(chan error, 1)
	go func() { completed <- cmd.Wait() }()
	select {
	case err := <-completed:
		if err == nil || !strings.Contains(output.String(), expected) {
			t.Fatalf("controlled restart guard did not reject %s: %s", expected, output.String())
		}
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		<-completed
		t.Fatal("invalid restart fixture became a serving process")
	}
}

func startRestartChild(t *testing.T, directory, phase, model string) (*exec.Cmd, restartAccess) {
	t.Helper()
	cmd := restartChildCommand(t, directory, phase, model)
	log, err := os.CreateTemp(t.TempDir(), "restart-child-*.log")
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait(); _ = log.Close() })
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(filepath.Join(directory, "fixture-access.json"))
		var access restartAccess
		if err == nil && json.Unmarshal(raw, &access) == nil && access.PID == cmd.Process.Pid {
			return cmd, access
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Controlled fixture logs contain only synthetic data, no real credentials.
	raw, _ := os.ReadFile(log.Name())
	t.Fatalf("controlled restart child did not become ready: %s", raw)
	return nil, restartAccess{}
}

func TestAgentControlledProcessCrashRestart(t *testing.T) {
	var calls atomic.Int32
	blocked := make(chan struct{}, 1)
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if calls.Add(1) == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: "+`{"model":"controlled-restart-model","choices":[{"delta":{"tool_calls":[{"index":0,"id":"restart-query","type":"function","function":{"name":"query_data","arguments":"{\"sql\":\"SELECT COUNT(*) AS lead_count FROM leads\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":100,"completion_tokens":30}}`+"\n\ndata: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"UNVERIFIED_RESTART_DRAFT\",\"reasoning_content\":\"PRIVATE_RESTART_REASONING\"}}]}\n\n")
		w.(http.Flusher).Flush()
		select {
		case blocked <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	}))
	defer model.Close()
	directory := filepath.Join(t.TempDir(), "crm-agent-restart-controlled")
	child, access := startRestartChild(t, directory, "init", model.URL+"/v1")
	rejectRestartChild(t, directory, "resume", model.URL+"/v1", "already belongs to a running process")
	var conversation agent.Conversation
	restartRequest(t, access, "POST", "/api/agent/conversations", map[string]any{"title": "隔离进程恢复"}, &conversation)
	var running agent.Run
	restartRequest(t, access, "POST", "/api/agent/conversations/"+conversation.ID+"/runs", map[string]any{"message": "查询线索总量", "request_id": uuid.NewString()}, &running)
	select {
	case <-blocked:
	case <-time.After(15 * time.Second):
		t.Fatal("controlled request did not reach the second streaming model call")
	}
	var before restartObservation
	restartRequest(t, access, "GET", "/__restart_observation", nil, &before)
	if len(before.Reservations) != 2 || len(before.ResultSHA) != 1 || before.Snapshots == 0 || calls.Load() != 2 {
		t.Fatalf("must crash a real in-flight query with saved result, settled/unknown calls and snapshot: reservations=%d results=%d snapshots=%d calls=%d", len(before.Reservations), len(before.ResultSHA), before.Snapshots, calls.Load())
	}
	if len(before.Runs) != 1 || before.Runs[0].ID != running.ID || before.Runs[0].Status != "running" {
		t.Fatal("the crash probe must interrupt an actual running HTTP request")
	}
	statuses := map[string]int{}
	for _, item := range before.Reservations {
		statuses[item.Status]++
	}
	if statuses["reserved"] != 1 || statuses["settled"] != 1 {
		t.Fatalf("unexpected pre-crash ledger: %v", statuses)
	}
	// SIGKILL is essential: a graceful Close would cancel/settle the run and
	// exercise a different lifecycle from a process crash.
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	rejectRestartChild(t, directory, "init", model.URL+"/v1", "previously nonexistent directory")
	rejectRestartChild(t, directory, "resume", model.URL+"/changed", "identity changed")
	identityPath := filepath.Join(directory, "identity.json")
	if err := os.Chmod(identityPath, 0644); err != nil {
		t.Fatal(err)
	}
	rejectRestartChild(t, directory, "resume", model.URL+"/v1", "regular private 0600 file")
	if err := os.Chmod(identityPath, 0600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(filepath.Dir(directory), "crm-agent-restart-symlink")
	if err := os.Symlink(directory, symlink); err != nil {
		t.Fatal(err)
	}
	rejectRestartChild(t, symlink, "resume", model.URL+"/v1", "real private 0700 directory")
	// An explicit seeded queue probe covers the pre-worker state. Its identity
	// is separate from the HTTP-started, real in-flight run and it makes no call.
	conn, err := sql.Open("sqlite3", access.Database+"?_busy_timeout=5000")
	if err != nil {
		t.Fatal(err)
	}
	queued := uuid.NewString()
	_, err = conn.Exec(`INSERT INTO ai_runs (id,conversation_id,account_id,request_id,auth_version,status,model,rule_version,created_at) VALUES (?,?,9000000,?,1,'queued','controlled-restart-model',0,?)`, queued, conversation.ID, uuid.NewString(), time.Now().UTC())
	if err == nil {
		_, err = conn.Exec(`INSERT INTO ai_messages (id,conversation_id,run_id,role,content,blocks_json,status,created_at) VALUES (?,?,?,'assistant','','[]','running',?)`, uuid.NewString(), conversation.ID, queued, time.Now().UTC())
	}
	_ = conn.Close()
	if err != nil {
		t.Fatal(err)
	}
	for restart := 0; restart < 2; restart++ {
		next, resumed := startRestartChild(t, directory, "resume", model.URL+"/v1")
		var after restartObservation
		restartRequest(t, resumed, "GET", "/__restart_observation", nil, &after)
		if !reflect.DeepEqual(before.Reservations, after.Reservations) || !reflect.DeepEqual(before.ResultSHA, after.ResultSHA) || before.BusinessSHA != after.BusinessSHA || after.Snapshots != 0 || after.DeltaEvents != 0 {
			t.Fatal("restart changed ledger/results/business, retained snapshots or exposed draft events")
		}
		if len(after.Runs) != 2 {
			t.Fatalf("runs duplicated during restart: %d", len(after.Runs))
		}
		for _, id := range []string{running.ID, queued} {
			var response struct {
				Run      agent.Run `json:"run"`
				Messages []struct {
					Role, Content, Status string
					Blocks                []agent.Block
				} `json:"messages"`
				Selection struct {
					ResultIDs []string `json:"result_ids"`
				} `json:"result_selection"`
			}
			restartRequest(t, resumed, "GET", "/api/agent/runs/"+id, nil, &response)
			if response.Run.Status != "interrupted" || after.DoneEvents[id] != 1 || len(response.Selection.ResultIDs) != 0 {
				t.Fatal("recovery failed or incorrectly promoted unfinished results")
			}
			for _, message := range response.Messages {
				if strings.Contains(message.Content, "UNVERIFIED_RESTART_DRAFT") || strings.Contains(message.Content, "PRIVATE_RESTART_REASONING") || (message.Role == "assistant" && (message.Status != "interrupted" || len(message.Blocks) != 0)) {
					t.Fatal("restarted API exposed an unverified answer or reasoning")
				}
			}
		}
		time.Sleep(150 * time.Millisecond)
		if calls.Load() != 2 {
			t.Fatal("restart automatically called the model")
		}
		if err := next.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		_ = next.Wait()
	}
}
