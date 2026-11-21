package main

// This opt-in test is intentionally compiled out of production binaries. It
// never opens the production database as the business/snapshot data source.
// Only settings + the configuring administrator may be read, and a separate
// authorizer-restricted connection may write the shared cost ledger.
import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/agent"
	"github.com/lsaac128/kxone/internal/security"
	"github.com/mattn/go-sqlite3"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const liveLedgerDriver = "crm_live_acceptance_ledger_only"
const liveSettingsDriver = "crm_live_acceptance_settings_only"

func init() {
	sql.Register(liveLedgerDriver, &sqlite3.SQLiteDriver{ConnectHook: func(c *sqlite3.SQLiteConn) error {
		c.RegisterAuthorizer(func(op int, first, second, database string) int {
			switch op {
			case sqlite3.SQLITE_SELECT, sqlite3.SQLITE_TRANSACTION, sqlite3.SQLITE_SAVEPOINT:
				return sqlite3.SQLITE_OK
			case sqlite3.SQLITE_READ, sqlite3.SQLITE_INSERT, sqlite3.SQLITE_UPDATE:
				if database == "main" && first == "ai_budget_reservations" {
					return sqlite3.SQLITE_OK
				}
			case sqlite3.SQLITE_FUNCTION:
				if second == "sum" || second == "coalesce" || second == "sqlite_version" {
					return sqlite3.SQLITE_OK
				}
			}
			return sqlite3.SQLITE_DENY
		})
		return nil
	}})
	sql.Register(liveSettingsDriver, &sqlite3.SQLiteDriver{ConnectHook: func(c *sqlite3.SQLiteConn) error {
		c.RegisterAuthorizer(func(op int, first, second, database string) int {
			if op == sqlite3.SQLITE_SELECT {
				return sqlite3.SQLITE_OK
			}
			if op == sqlite3.SQLITE_READ && database == "main" {
				if first == "ai_settings" && liveAllowedSettingColumn(second) {
					return sqlite3.SQLITE_OK
				}
				if first == "crm_wecom_user_bindings" && (second == "id" || second == "auth_version" || second == "role" || second == "status") {
					return sqlite3.SQLITE_OK
				}
			}
			return sqlite3.SQLITE_DENY
		})
		return nil
	}})
}

func liveAllowedSettingColumn(name string) bool {
	return strings.Contains("|id|budget_micro|base_url|model|key_ciphertext|input_price_micro|output_price_micro|price_version|updated_by|version|", "|"+name+"|")
}

type liveFixture struct {
	Version             string                      `json:"version"`
	Dataset             string                      `json:"dataset,omitempty"`
	BaseFixtureHash     string                      `json:"base_fixture_sha256,omitempty"`
	SemanticExpected    map[string]any              `json:"-"`
	ContentHash         string                      `json:"-"`
	ReferenceNow        string                      `json:"reference_now"`
	Tables              map[string][]map[string]any `json:"tables"`
	IndependentExpected map[string]any              `json:"independent_expected"`
}

func loadLiveFixture(t *testing.T) (liveFixture, string) {
	t.Helper()
	path := os.Getenv("KXONE_AGENT_LIVE_FIXTURE")
	if path == "" {
		path = "../../docs/Agent-Evaluation-Fixture.json"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture liveFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Version != "agent-evaluation-fixture-v1" || len(fixture.Tables["leads"]) != 7 {
		t.Fatal("unrecognized live fixture; no cloud call is allowed")
	}
	hash := sha256.Sum256(raw)
	return fixture, hex.EncodeToString(hash[:])
}

func loadSelectedLiveFixture(t *testing.T) (liveFixture, string) {
	t.Helper()
	switch os.Getenv("KXONE_AGENT_LIVE_DATASET") {
	case "", "gold-v1":
		return loadLiveFixture(t)
	case "stress-v1":
		return loadLiveStressFixture(t)
	case liveDailyDataset:
		return loadLiveDailyFixture(t)
	case liveProdCopyDataset:
		return loadLiveProdCopyFixture(t)
	case "semantic-active-timestamp-v1", "semantic-missing-timestamp-v1":
		return loadLiveSemanticFixture(t, os.Getenv("KXONE_AGENT_LIVE_DATASET"))
	default:
		t.Fatal("unrecognized LIVE_DATASET")
		return liveFixture{}, ""
	}
}

// The fixture is data, not schema. Import it into the exact production models
// and fail for unknown fields rather than silently omitting evaluation facts.
func importLiveFixture(t *testing.T, db *gorm.DB, fixture liveFixture, adminID int64) {
	t.Helper()
	reference, err := time.Parse(time.RFC3339, fixture.ReferenceNow)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"crm_wecom_user_bindings", "leads", "lead_followups", "lead_dispatch_records", "lead_appointments", "lead_activity_records", "lead_reminder_records"} {
		var schema []struct {
			Name      string
			Type      string
			Notnull   int
			DfltValue *string `gorm:"column:dflt_value"`
		}
		if err := db.Raw("PRAGMA table_info(" + name + ")").Scan(&schema).Error; err != nil || len(schema) == 0 {
			t.Fatalf("fixture schema %s unavailable: %v", name, err)
		}
		columns := map[string]bool{}
		for _, col := range schema {
			columns[col.Name] = true
		}
		for index, original := range fixture.Tables[name] {
			row := make(map[string]any, len(original)+4)
			for key, value := range original {
				if !columns[key] {
					t.Fatalf("fixture %s field %s does not exist in actual schema", name, key)
				}
				if text, ok := value.(string); ok && (strings.HasSuffix(key, "_at") || key == "start_at" || key == "end_at") {
					value, err = time.Parse(time.RFC3339, text)
					if err != nil {
						t.Fatalf("fixture invalid timestamp %s.%s", name, key)
					}
				}
				row[key] = value
			}
			for _, col := range schema {
				if _, exists := row[col.Name]; exists || col.Notnull == 0 || col.DfltValue != nil {
					continue
				}
				switch {
				case strings.Contains(strings.ToLower(col.Type), "int"):
					row[col.Name] = 0
				case strings.Contains(strings.ToLower(col.Type), "datetime"):
					row[col.Name] = reference
				default:
					row[col.Name] = ""
				}
			}
			switch name {
			case "crm_wecom_user_bindings":
				// Preserve all textual business IDs; avoid colliding with the real
				// administrator ID used only for cost attribution.
				row["id"] = adminID + 1000 + int64(index)
				row["crm_user_id"] = adminID + 1000 + int64(index)
				row["username"] = row["wecom_userid"]
			case "lead_appointments":
				row["appointment_no"] = fmt.Sprintf("TEST-APPT-%d", index+1)
			case "lead_reminder_records":
				row["idempotency_key"] = fmt.Sprintf("TEST-REMINDER-%d", index+1)
			}
			if err := db.Table(name).Create(row).Error; err != nil {
				t.Fatalf("import fixture %s row %d: %v", name, index, err)
			}
		}
	}
	for index, role := range []string{"admin", "manager"} {
		account := repo.WeComUserBindingModel{ID: adminID + int64(index), CRMUserID: adminID + int64(index), Username: fmt.Sprintf("live-evaluation-%s", role), WeComUserID: fmt.Sprintf("live-evaluation-%s", role), Role: role, Status: "active", AuthVersion: 1, AccountVersion: 1, EmployeeName: "隔离验收账号", CreatedAt: reference, UpdatedAt: reference}
		if err := db.Create(&account).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func openLiveLedger(t *testing.T, path string) *gorm.DB {
	t.Helper()
	uri := (&url.URL{Scheme: "file", Path: path}).String() + "?mode=rw&_busy_timeout=5000"
	sqlDB, err := sql.Open(liveLedgerDriver, uri)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(2)
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(sqlite.Dialector{Conn: sqlDB}, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	// Verify the enforcement boundary before permitting any paid call.
	for _, query := range []string{"SELECT id FROM leads LIMIT 1", "SELECT password_hash FROM crm_wecom_user_bindings LIMIT 1", "UPDATE leads SET version=version WHERE 0", "DELETE FROM ai_budget_reservations WHERE 0", "PRAGMA user_version", "ATTACH ':memory:' AS forbidden"} {
		if _, err := sqlDB.Exec(query); err == nil {
			t.Fatalf("ledger connection incorrectly permits %s", query)
		}
	}
	if _, err := agent.NewBudget(db).Summary(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	return db
}

func readLiveConfig(t *testing.T, sourcePath string) (agent.Config, int64, int64) {
	t.Helper()
	key := os.Getenv("FIELD_ENCRYPTION_KEY")
	if key == "" {
		key = os.Getenv("PII_ENCRYPTION_KEY")
	}
	if key == "" {
		t.Fatal("FIELD_ENCRYPTION_KEY is required in the process environment; do not put it in command arguments or evidence")
	}
	protector, err := security.NewAESGCMFieldProtector(key)
	if err != nil {
		t.Fatal("invalid field encryption key")
	}
	uri := (&url.URL{Scheme: "file", Path: sourcePath}).String() + "?mode=ro&_query_only=1"
	db, err := sql.Open(liveSettingsDriver, uri)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var cfg = agent.DefaultConfig()
	// Phase two acceptance runs the production default: fixed metrics.
	cfg.FixedMetrics = os.Getenv("KXONE_AGENT_FREE_ANALYSIS") != "1"
	var cipher string
	var actorID, version, actorVersion int64
	var role, status string
	err = db.QueryRow(`SELECT s.budget_micro,s.base_url,s.model,s.key_ciphertext,s.input_price_micro,s.output_price_micro,s.price_version,s.updated_by,s.version,u.auth_version,u.role,u.status FROM ai_settings s JOIN crm_wecom_user_bindings u ON u.id=s.updated_by WHERE s.id=1`).Scan(&cfg.MonthlyBudgetMicro, &cfg.BaseURL, &cfg.Model, &cipher, &cfg.InputPriceMicro, &cfg.OutputPriceMicro, &cfg.PriceVersion, &actorID, &version, &actorVersion, &role, &status)
	if err != nil {
		t.Fatal("cannot read approved saved model settings: ", err)
	}
	if actorID <= 0 || actorVersion <= 0 || role != "admin" || status != "active" {
		t.Fatal("saved model settings must belong to an active administrator")
	}
	if cfg.MonthlyBudgetMicro <= 0 || cfg.InputPriceMicro <= 0 || cfg.OutputPriceMicro <= 0 || cfg.Model == "" || cfg.BaseURL == "" || !security.IsEncryptedField(cipher) {
		t.Fatal("saved model configuration is incomplete")
	}
	cfg.APIKey, err = protector.Reveal(cipher)
	if err != nil || cfg.APIKey == "" {
		t.Fatal("saved model key cannot be decrypted")
	}
	cfg.Enabled = true // isolated service only; never update production settings
	if limit := os.Getenv("KXONE_AGENT_LIVE_RUN_BUDGET_MICRO"); limit != "" {
		ceiling, err := strconv.ParseInt(limit, 10, 64)
		if err != nil || ceiling <= 0 || ceiling > cfg.RunBudgetMicro {
			t.Fatal("live acceptance budget may only tighten the production default")
		}
		cfg.RunBudgetMicro = ceiling
	}
	return cfg, actorID, version
}

// Recheck the saved configuration and its real administrator immediately before
// every paid outbound request. This connection cannot read customer fields,
// passwords, or make changes; a changed configuration needs a fresh fixture.
func checkLiveSourceUnchanged(sourcePath string, actorID, version int64) error {
	uri := (&url.URL{Scheme: "file", Path: sourcePath}).String() + "?mode=ro&_query_only=1"
	db, err := sql.Open(liveSettingsDriver, uri)
	if err != nil {
		return errors.New("acceptance source verification unavailable")
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var currentVersion, updatedBy int64
	var role, status string
	err = db.QueryRowContext(ctx, `SELECT s.version,s.updated_by,u.role,u.status FROM ai_settings s JOIN crm_wecom_user_bindings u ON u.id=? WHERE s.id=1`, actorID).Scan(&currentVersion, &updatedBy, &role, &status)
	if err != nil || currentVersion != version || updatedBy != actorID || role != "admin" || status != "active" {
		return errors.New("acceptance saved configuration or administrator changed; restart after verification")
	}
	return nil
}

func writeLiveEvidence(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
}

func liveDiagnosticRedact(body []byte, key string, fixture liveFixture) string {
	if strings.Contains(string(body), `"reasoning_content"`) {
		return "[reasoning-bearing provider response omitted from diagnostics]"
	}
	// Decode JSON string escapes before matching known private values. For a
	// truncated or non-JSON provider error, retain only the redacted text.
	var parsed any
	if json.Unmarshal(body, &parsed) == nil {
		if normalized, err := json.Marshal(parsed); err == nil {
			body = normalized
		}
	}
	text := string(body)
	fragments := []string{key}
	for _, value := range fixture.IndependentExpected["forbidden_provider_payload_fragments"].([]any) {
		fragments = append(fragments, value.(string))
	}
	for _, row := range fixture.Tables["leads"] {
		for _, field := range []string{"customer_name", "customer_mobile", "customer_wechat"} {
			if value, ok := row[field].(string); ok && value != "" {
				fragments = append(fragments, value)
			}
		}
	}
	sort.Slice(fragments, func(i, j int) bool { return len(fragments[i]) > len(fragments[j]) })
	for _, fragment := range fragments {
		if fragment != "" {
			text = strings.ReplaceAll(text, fragment, "[REDACTED]")
			encoded, _ := json.Marshal(fragment)
			if len(encoded) > 2 {
				text = strings.ReplaceAll(text, string(encoded[1:len(encoded)-1]), "[REDACTED]")
			}
		}
	}
	// Error bodies may end mid-key at the 8 KiB bound. Mask recognizable key
	// prefixes/suffixes too, never keeping a credential fragment for diagnosis.
	for n := len(key) - 1; n >= 8; n-- {
		text = strings.ReplaceAll(text, key[:n], "[REDACTED]")
		text = strings.ReplaceAll(text, key[len(key)-n:], "[REDACTED]")
	}
	return text
}

// Run on the server with its environment already loaded, through an SSH
// loopback tunnel. No secret is written to fixture-access.json or test output.
// The test starts no model request: a browser/HTTP driver must explicitly ask.
func TestAgentLiveAcceptance(t *testing.T) {
	if os.Getenv("KXONE_AGENT_LIVE_ACCEPTANCE") != "1" {
		t.Skip("explicit opt-in required for real model acceptance")
	}
	source := os.Getenv("KXONE_AGENT_LIVE_SOURCE_DB")
	evidence := os.Getenv("KXONE_AGENT_LIVE_EVIDENCE_DIR")
	listen := os.Getenv("KXONE_AGENT_LIVE_LISTEN")
	if source == "" || evidence == "" || listen == "" {
		t.Fatal("LIVE_SOURCE_DB, LIVE_EVIDENCE_DIR and LIVE_LISTEN are required")
	}
	if !filepath.IsAbs(source) || !filepath.IsAbs(evidence) {
		t.Fatal("live paths must be absolute")
	}
	host, _, err := net.SplitHostPort(listen)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		t.Fatal("acceptance server must bind an explicit loopback IP")
	}
	if err := os.MkdirAll(evidence, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(evidence, 0700); err != nil {
		t.Fatal(err)
	}
	fixture, fixtureHash := loadSelectedLiveFixture(t)
	cfg, actorID, settingVersion := readLiveConfig(t, source)
	cfg, fallbackPath, fallbackHash := loadLiveFallback(t, cfg)
	cfg, faultMetadata, faultCount := livePrimaryFault(t, cfg)
	cfg.Directory = filepath.Join(evidence, "isolated-agent-runtime")
	var auditMu sync.Mutex
	cfg.AnswerValidationAudit = func(record agent.AnswerValidationAuditRecord) {
		auditMu.Lock()
		defer auditMu.Unlock()
		file, err := os.OpenFile(filepath.Join(evidence, "answer-validation-audit.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			t.Error("isolated final-answer audit unavailable")
			return
		}
		defer file.Close()
		if file.Chmod(0600) != nil || json.NewEncoder(file).Encode(map[string]any{"at": time.Now().UTC(), "validation": record}) != nil {
			t.Error("isolated final-answer audit write failed")
		}
	}
	cfg.ProviderErrorAudit = func(status int, body []byte) {
		auditMu.Lock()
		defer auditMu.Unlock()
		entry := map[string]any{"at": time.Now().UTC(), "status": status, "body": liveDiagnosticRedact(body, cfg.APIKey, fixture), "body_limit_bytes": 8 << 10, "possibly_truncated": len(body) >= 8<<10}
		file, err := os.OpenFile(filepath.Join(evidence, "provider-errors.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return
		}
		defer file.Close()
		if file.Chmod(0600) != nil {
			return
		}
		_ = json.NewEncoder(file).Encode(entry)
	}
	cfg.RequestAudit = func(body []byte) error {
		auditMu.Lock()
		defer auditMu.Unlock()
		hash := sha256.Sum256(body)
		entry := map[string]any{"at": time.Now().UTC(), "request_sha256": hex.EncodeToString(hash[:]), "bytes": len(body), "privacy_passed": true}
		blocked := false
		if fallbackPath != "" {
			raw, err := os.ReadFile(fallbackPath)
			digest := sha256.Sum256(raw)
			entry["fallback_profile_current"] = err == nil && hex.EncodeToString(digest[:]) == fallbackHash
			if entry["fallback_profile_current"] != true {
				blocked = true
			}
		}
		if err := checkLiveSourceUnchanged(source, actorID, settingVersion); err != nil {
			entry["source_config_current"], blocked = false, true
		} else {
			entry["source_config_current"] = true
		}
		for index, fragment := range fixture.IndependentExpected["forbidden_provider_payload_fragments"].([]any) {
			if strings.Contains(string(body), fragment.(string)) {
				entry["privacy_passed"], entry["forbidden_fixture_index"], blocked = false, index, true
				break
			}
		}
		if strings.Contains(string(body), cfg.APIKey) {
			entry["privacy_passed"], entry["key_in_body"], blocked = false, true, true
		}
		if !blocked {
			// Already approved, synthetic requests only. No HTTP headers are
			// available here. Private capture diagnoses provider tool protocols.
			var audited map[string]any
			_ = json.Unmarshal(body, &audited)
			if messages, ok := audited["messages"].([]any); ok {
				for _, message := range messages {
					if item, ok := message.(map[string]any); ok {
						delete(item, "reasoning_content")
					}
				}
			}
			entry["synthetic_request"] = audited
		}
		file, err := os.OpenFile(filepath.Join(evidence, "outbound-audit.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return errors.New("acceptance audit unavailable")
		}
		if err := file.Chmod(0600); err != nil {
			_ = file.Close()
			return errors.New("acceptance audit unavailable")
		}
		err = json.NewEncoder(file).Encode(entry)
		_ = file.Close()
		if err != nil || blocked {
			return errors.New("acceptance outbound guard rejected the request")
		}
		return nil
	}
	ledger := openLiveLedger(t, source)
	var fixtureDB *gorm.DB
	prodCopyKey := ""
	if fixture.Version == liveProdCopyDataset {
		prodCopyKey = liveFieldKey(t)
	}
	router := newTestRouterWithOptions(t, testRouterOptions{agentConfig: &cfg, agentPublicModel: true, agentFieldKey: prodCopyKey, agentFixtureDB: &fixtureDB, agentServiceOptions: []agent.ServiceOption{agent.WithBudget(agent.NewBudget(ledger))}, agentPrepareFixture: func(t *testing.T, db *gorm.DB) {
		if fixture.Version == liveStressVersion {
			importLiveStressFixture(t, db, fixture, actorID)
		} else if fixture.Version == liveProdCopyDataset {
			importLiveProdCopy(t, db, actorID)
		} else {
			importLiveFixture(t, db, fixture, actorID)
		}
	}})
	token := issueDevToken(t, router, "live-evaluation-admin", "admin")
	second := issueDevToken(t, router, "live-evaluation-manager", "manager")
	fingerprintContext, fingerprintCancel := context.WithTimeout(context.Background(), 30*time.Second)
	beforeBusiness, err := captureLiveBusinessFingerprint(fingerprintContext, fixtureDB)
	fingerprintCancel()
	if err != nil {
		t.Fatalf("initial isolated business fingerprint unavailable: %v", err)
	}
	writeLiveEvidence(t, filepath.Join(evidence, "business-before.json"), beforeBusiness)
	var businessIntegrity *liveBusinessIntegrity
	var fingerprintAfterAttempted bool
	captureAfterBusiness := func() {
		if fingerprintAfterAttempted {
			return
		}
		fingerprintAfterAttempted = true
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		after, err := captureLiveBusinessFingerprint(ctx, fixtureDB)
		if err != nil {
			writeLiveEvidence(t, filepath.Join(evidence, "business-integrity.json"), map[string]any{"status": "after_capture_failed", "before_sha256": beforeBusiness.SHA256, "consistent": nil, "full_database_proof": false})
			t.Errorf("final isolated business fingerprint unavailable: %v", err)
			return
		}
		comparison := compareLiveBusinessFingerprints(beforeBusiness, after)
		businessIntegrity = &comparison
		writeLiveEvidence(t, filepath.Join(evidence, "business-after.json"), after)
		writeLiveEvidence(t, filepath.Join(evidence, "business-integrity.json"), comparison)
		if !comparison.Consistent {
			t.Errorf("isolated business tables changed during analysis: %v", comparison.ChangedTables)
		}
	}
	// Registered before the server cleanup, so HTTP is stopped before the
	// fallback capture, while the router's temporary DB still exists.
	t.Cleanup(captureAfterBusiness)
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		// The second account is solely a privacy probe; paid calls are charged
		// only to the administrator whose saved configuration was validated.
		if r.Method == http.MethodPost && (strings.HasSuffix(r.URL.Path, "/runs") || strings.HasSuffix(r.URL.Path, "/settings/test")) {
			access := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if access == "" {
				if cookie, err := r.Cookie("kxone_access_token"); err == nil {
					access = cookie.Value
				}
			}
			if access != token {
				http.Error(w, "paid acceptance requests require the validated isolated administrator", http.StatusForbidden)
				return
			}
		}
		router.ServeHTTP(w, r)
	})
	dist := os.Getenv("KXONE_AGENT_LIVE_WEB_DIST")
	if dist == "" {
		dist = "../../web/dist"
	}
	if _, err := os.Stat(filepath.Join(dist, "index.html")); err != nil {
		t.Fatal("built Agent web/dist is required for live browser acceptance")
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			http.FileServer(http.Dir(dist)).ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(dist, "index.html"))
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})
	baseURL := "http://" + listener.Addr().String()
	dataset := "gold-v1"
	if fixture.Version == liveStressVersion {
		dataset = "stress-v1"
	}
	if fixture.Dataset != "" {
		dataset = fixture.Dataset
	}
	candidateIdentity := liveCandidateIdentity(t, fixtureDB, cfg, dist)
	candidateIdentity["business_sha256"] = beforeBusiness.SHA256
	writeLiveEvidence(t, filepath.Join(evidence, "fixture-access.json"), map[string]any{"candidate_identity": candidateIdentity, "base_url": baseURL, "token": token, "second_token": second, "account": map[string]any{"id": actorID, "username": "live-evaluation-admin", "role": "admin"}, "second_account": map[string]any{"id": actorID + 1, "username": "live-evaluation-manager", "role": "manager"}, "model": cfg.Model, "fallback_model": liveFallbackModel(cfg), "fault_injection": faultMetadata, "provider_target": os.Getenv("KXONE_AGENT_LIVE_PROVIDER_TARGET"), "fixture_hash": fixtureHash, "dataset": dataset, "fixture_version": fixture.Version, "fixture_content_sha256": fixture.ContentHash, "base_fixture_sha256": fixture.BaseFixtureHash})
	metadata := map[string]any{"started_at": time.Now().UTC(), "configured_model": cfg.Model, "provider_reported_model": "unknown until a model_usage event reports it", "base_url": cfg.BaseURL, "settings_version": settingVersion, "fixture_version": fixture.Version, "fixture_sha256": fixtureHash, "code_revision": os.Getenv("KXONE_AGENT_LIVE_REVISION"), "input_price_micro": cfg.InputPriceMicro, "output_price_micro": cfg.OutputPriceMicro, "price_version": cfg.PriceVersion, "price_basis": "user configured; not vendor invoice verified", "monthly_budget_micro": cfg.MonthlyBudgetMicro, "run_budget_micro": cfg.RunBudgetMicro, "status": "awaiting_explicit_http_questions", "acceptance_passed": false, "manual_review_required": true}
	metadata["fallback_profile_sha256"], metadata["provider_target"] = fallbackHash, os.Getenv("KXONE_AGENT_LIVE_PROVIDER_TARGET")
	metadata["fallback_model"] = liveFallbackModel(cfg)
	metadata["fault_injection"] = faultMetadata
	metadata["dataset"], metadata["fixture_content_sha256"] = dataset, fixture.ContentHash
	metadata["business_before_sha256"], metadata["business_fingerprint_scope"] = beforeBusiness.SHA256, beforeBusiness.Scope
	metadata["business_full_database_proof"] = false
	if fixture.SemanticExpected != nil {
		metadata["base_fixture_sha256"], metadata["semantic_expected"] = fixture.BaseFixtureHash, fixture.SemanticExpected
	}
	writeLiveEvidence(t, filepath.Join(evidence, "acceptance-metadata.json"), metadata)
	t.Logf("Live acceptance fixture ready at %s; only synthetic business data; evidence metadata contains no cloud key", baseURL)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ttl := 90 * time.Minute
	if value := os.Getenv("KXONE_AGENT_LIVE_TTL_SECONDS"); value != "" {
		n, e := strconv.Atoi(value)
		if e != nil || n < 1 || n > 7200 {
			t.Fatal("TTL must be 1..7200 seconds")
		}
		ttl = time.Duration(n) * time.Second
	}
	select {
	case <-ctx.Done():
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Fatal(err)
		}
	case <-time.After(ttl):
	}
	var runs []agent.Run
	if err := fixtureDB.Order("created_at").Find(&runs).Error; err != nil {
		t.Fatal(err)
	}
	for _, run := range runs {
		if run.Status == "running" {
			_ = doJSON(t, router, http.MethodPost, "/api/agent/runs/"+run.ID+"/cancel", token, map[string]any{})
		}
	}
	shutdown, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	_ = server.Shutdown(shutdown)
	cancelShutdown()
	if err := fixtureDB.Order("created_at").Find(&runs).Error; err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, run := range runs {
		ids = append(ids, run.ID)
	}
	var charges []agent.BudgetReservation
	if len(ids) > 0 {
		if err := ledger.Where("run_id IN ?", ids).Find(&charges).Error; err != nil {
			t.Fatal(err)
		}
	}
	writeLiveEvidence(t, filepath.Join(evidence, "run-ledger.json"), map[string]any{"runs": runs, "charges": charges, "acceptance_passed": false, "manual_review_required": true})
	var events []agent.Event
	var messages []agent.Message
	var results []agent.ResultRecord
	if err := fixtureDB.Order("seq").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixtureDB.Order("created_at").Find(&messages).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixtureDB.Order("created_at").Find(&results).Error; err != nil {
		t.Fatal(err)
	}
	eventOutput := []map[string]any{}
	models := map[string]bool{}
	for _, event := range events {
		var data any
		if json.Unmarshal([]byte(event.Data), &data) != nil {
			data = event.Data
		}
		eventOutput = append(eventOutput, map[string]any{"seq": event.Seq, "run_id": event.RunID, "type": event.Type, "data": data, "created_at": event.CreatedAt})
		if event.Type == "model_usage" {
			if fields, ok := data.(map[string]any); ok {
				if value, ok := fields["reported_model"].(string); ok && value != "" {
					models[value] = true
				}
			}
		}
	}
	messageOutput := []map[string]any{}
	for _, message := range messages {
		messageOutput = append(messageOutput, message.DTO())
	}
	writeLiveEvidence(t, filepath.Join(evidence, "events.json"), eventOutput)
	writeLiveEvidence(t, filepath.Join(evidence, "messages.json"), messageOutput)
	writeLiveEvidence(t, filepath.Join(evidence, "results.json"), results)
	var reportedModels []string
	for model := range models {
		reportedModels = append(reportedModels, model)
	}
	sort.Strings(reportedModels)
	if len(reportedModels) > 0 {
		metadata["provider_reported_model"] = reportedModels
	}
	captureAfterBusiness()
	if businessIntegrity != nil {
		metadata["business_after_sha256"], metadata["business_tables_consistent"] = businessIntegrity.After.SHA256, businessIntegrity.Consistent
	}
	if faultCount != nil {
		metadata["injected_primary_failure_count"] = faultCount.Load()
	}
	metadata["finished_at"], metadata["status"], metadata["run_count"] = time.Now().UTC(), "captured_requires_independent_and_manual_review", len(runs)
	writeLiveEvidence(t, filepath.Join(evidence, "acceptance-metadata.json"), metadata)
}

// This always-runnable test proves the business fixture imports into real
// schema and independently checks the expected numbers before any paid test.
func TestAgentLiveFixtureIndependentExpectations(t *testing.T) {
	fixture, _ := loadLiveFixture(t)
	var db *gorm.DB
	newTestRouterWithOptions(t, testRouterOptions{agentFixtureDB: &db, agentPrepareFixture: func(t *testing.T, db *gorm.DB) { importLiveFixture(t, db, fixture, 1) }})
	checks := []struct {
		sql  string
		want int64
	}{
		{"SELECT count(*) FROM leads", 7}, {"SELECT count(*) FROM lead_followups", 7},
		{"SELECT count(*) FROM leads WHERE status='converted'", 2},
		{"SELECT CAST(ROUND(SUM(deal_amount-refund_amount)*100) AS INTEGER) FROM leads", 1300000},
		{"SELECT count(*) FROM lead_followups WHERE sales_user_id='sales-a'", 6},
		{"SELECT count(*) FROM lead_followups WHERE sales_user_id='sales-b'", 1},
		{"SELECT count(*) FROM lead_followups WHERE remark LIKE '%费用%'", 3},
	}
	for _, check := range checks {
		var got int64
		if err := db.Raw(check.sql).Scan(&got).Error; err != nil || got != check.want {
			t.Fatalf("independent check %s got=%d want=%d error=%v", check.sql, got, check.want, err)
		}
	}
	snapshot, err := agent.BuildSnapshot(t.Context(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	for _, check := range []struct {
		sql  string
		want [][]any
	}{
		{"SELECT id FROM leads WHERE current_followup_eligible=1 ORDER BY id", [][]any{{int64(7)}}},
		{"SELECT lifecycle_first_followup_seconds,owner_first_followup_seconds FROM leads WHERE id=1", [][]any{{int64(600), int64(1800)}}},
		{"SELECT id FROM leads WHERE created_at_date_shanghai='2026-10-08' ORDER BY id", [][]any{{int64(7)}}},
	} {
		result, err := snapshot.Query(t.Context(), check.sql, nil, 100)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := json.Marshal(result.Rows)
		want, _ := json.Marshal(check.want)
		if string(got) != string(want) {
			t.Fatalf("snapshot expectation %s got=%s want=%s", check.sql, got, want)
		}
	}
	for _, name := range []string{"leads", "lead_followups", "lead_dispatch_records", "lead_appointments", "lead_activity_records", "lead_reminder_records"} {
		result, err := snapshot.Query(t.Context(), "SELECT * FROM "+name, nil, 100)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(result)
		for _, fragment := range fixture.IndependentExpected["forbidden_provider_payload_fragments"].([]any) {
			if strings.Contains(string(raw), fragment.(string)) {
				t.Fatalf("sensitive fixture fragment survives in %s", name)
			}
		}
	}
}

func TestAgentLiveLedgerCannotAccessBusinessData(t *testing.T) {
	var db *gorm.DB
	newTestRouterWithOptions(t, testRouterOptions{agentFixtureDB: &db})
	var databaseFiles []struct{ File string }
	if err := db.Raw("PRAGMA database_list").Scan(&databaseFiles).Error; err != nil || len(databaseFiles) != 1 {
		t.Fatalf("fixture path unavailable: %v", err)
	}
	ledger := openLiveLedger(t, databaseFiles[0].File)
	budget := agent.NewBudget(ledger)
	id, err := budget.Reserve(t.Context(), "restricted-ledger-fixture", 1, 100000, 300000000, 3000000, "test-only", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.Settle(t.Context(), id, 50000, agent.Usage{InputTokens: 100, OutputTokens: 10}); err != nil {
		t.Fatal(err)
	}
	summary, err := budget.Summary(t.Context(), time.Now())
	if err != nil || summary.SettledMicro != 50000 {
		t.Fatalf("restricted ledger cannot settle: %+v %v", summary, err)
	}
}

func TestAgentLiveCredentialsUseEphemeralEncryption(t *testing.T) {
	cfg := agent.DefaultConfig()
	cfg.Directory = t.TempDir()
	cfg.BaseURL = "https://model.example.invalid/v1"
	cfg.APIKey = "synthetic-credential-for-encryption-test"
	var db *gorm.DB
	router := newTestRouterWithOptions(t, testRouterOptions{agentConfig: &cfg, agentPublicModel: true, agentFixtureDB: &db})
	var setting agent.Settings
	if err := db.First(&setting, 1).Error; err != nil {
		t.Fatal(err)
	}
	if !security.IsEncryptedField(setting.KeyCiphertext) || strings.Contains(setting.KeyCiphertext, cfg.APIKey) {
		t.Fatal("acceptance credential was not encrypted")
	}
	publicTestProtector, err := security.NewAESGCMFieldProtector("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := publicTestProtector.Reveal(setting.KeyCiphertext); err == nil {
		t.Fatal("real-provider fixture still uses the public unit-test field key")
	}
	admin := issueDevToken(t, router, "ephemeral-key-admin", "admin")
	response := doJSON(t, router, http.MethodGet, "/api/agent/settings", admin, nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"key_configured":true`) || strings.Contains(response.Body.String(), cfg.APIKey) {
		t.Fatal("service cannot use its ephemeral protector safely")
	}
}

func TestAgentLiveProviderErrorDiagnosticsRedactCredentialsAndFixture(t *testing.T) {
	fixture, _ := loadLiveFixture(t)
	const key = "synthetic-live-provider-secret-987654321"
	for _, body := range []string{
		`{"error":"` + key + ` 测试客户一 13800138001 test_parent_0001 0710-3456789"}`,
		`{"error":"` + key + ` \u6d4b\u8bd5\u5ba2\u6237\u4e00 13800138002 test_parent_0007"}`,
		"truncated error " + key[:len(key)-5],
	} {
		redacted := liveDiagnosticRedact([]byte(body), key, fixture)
		for _, forbidden := range []string{key, key[:len(key)-5], "测试客户一", "13800138001", "13800138002", "test_parent_0001", "test_parent_0007", "0710-3456789"} {
			if strings.Contains(redacted, forbidden) {
				t.Fatal("private fixture or credential fragment survived provider error redaction")
			}
		}
		if !strings.Contains(redacted, "[REDACTED]") {
			t.Fatal("diagnostic did not replace private content")
		}
	}
}

func TestAgentLiveSettingsReadOnlyApprovedColumns(t *testing.T) {
	fixture, _ := loadLiveFixture(t)
	var db *gorm.DB
	newTestRouterWithOptions(t, testRouterOptions{agentFixtureDB: &db, agentPrepareFixture: func(t *testing.T, db *gorm.DB) { importLiveFixture(t, db, fixture, 1) }})
	var files []struct{ File string }
	if err := db.Raw("PRAGMA database_list").Scan(&files).Error; err != nil || len(files) != 1 {
		t.Fatal("isolated path unavailable")
	}
	t.Setenv("FIELD_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	protector, _ := security.NewAESGCMFieldProtector(os.Getenv("FIELD_ENCRYPTION_KEY"))
	cipher, err := protector.Protect("synthetic-key-only")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&agent.Settings{}).Where("id=1").Updates(map[string]any{"base_url": "https://model.example.invalid/v1", "model": "synthetic-cloud-model", "key_ciphertext": cipher, "updated_by": 1, "version": 7}).Error; err != nil {
		t.Fatal(err)
	}
	cfg, accountID, version := readLiveConfig(t, files[0].File)
	if cfg.APIKey != "synthetic-key-only" || accountID != 1 || version != 7 {
		t.Fatal("approved setting import failed")
	}
	uri := (&url.URL{Scheme: "file", Path: files[0].File}).String() + "?mode=ro&_query_only=1"
	guarded, err := sql.Open(liveSettingsDriver, uri)
	if err != nil {
		t.Fatal(err)
	}
	defer guarded.Close()
	for _, query := range []string{"SELECT customer_name FROM leads", "SELECT password_hash FROM crm_wecom_user_bindings", "SELECT username FROM crm_wecom_user_bindings", "UPDATE ai_settings SET enabled=1 WHERE id=1", "PRAGMA user_version", "ATTACH ':memory:' AS forbidden"} {
		if _, err := guarded.Exec(query); err == nil {
			t.Fatalf("settings-only connection incorrectly permits %s", query)
		}
	}
	if err := checkLiveSourceUnchanged(files[0].File, 1, 7); err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct {
		name    string
		setting map[string]any
		account map[string]any
	}{
		{"version", map[string]any{"version": 8}, nil},
		{"configurator", map[string]any{"updated_by": 2}, nil},
		{"role", nil, map[string]any{"role": "manager"}},
		{"status", nil, map[string]any{"status": "disabled"}},
	} {
		if err := db.Model(&agent.Settings{}).Where("id=1").Updates(map[string]any{"version": 7, "updated_by": 1}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Table("crm_wecom_user_bindings").Where("id=1").Updates(map[string]any{"role": "admin", "status": "active"}).Error; err != nil {
			t.Fatal(err)
		}
		if change.setting != nil {
			if err := db.Model(&agent.Settings{}).Where("id=1").Updates(change.setting).Error; err != nil {
				t.Fatal(err)
			}
		}
		if change.account != nil {
			if err := db.Table("crm_wecom_user_bindings").Where("id=1").Updates(change.account).Error; err != nil {
				t.Fatal(err)
			}
		}
		if err := checkLiveSourceUnchanged(files[0].File, 1, 7); err == nil {
			t.Fatalf("outbound guard accepted changed %s", change.name)
		}
	}
}
