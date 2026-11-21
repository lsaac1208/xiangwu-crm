package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/lsaac128/kxone/internal/agent"
	"gorm.io/gorm"
)

// Identity is captured by the running opt-in fixture, never supplied by a
// runner wishing to reuse a pass. Hashes contain no credentials or rule text.
func liveCandidateIdentity(t *testing.T, db *gorm.DB, cfg agent.Config, root string) map[string]string {
	t.Helper()
	digest := func(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(binary)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		t.Fatal(err)
	}
	var paths []string
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			paths = append(paths, path)
		}
		return nil
	}); err != nil {
		t.Fatal("build CRM frontend before live acceptance:", err)
	}
	sort.Strings(paths)
	frontend := sha256.New()
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		relative, _ := filepath.Rel(root, path)
		frontend.Write([]byte(relative + "\x00" + digest(raw) + "\n"))
	}
	revision := os.Getenv("KXONE_AGENT_LIVE_REVISION")
	if revision == "" {
		raw, err := exec.Command("git", "rev-parse", "HEAD").Output()
		if err != nil {
			t.Fatal(err)
		}
		revision = strings.TrimSpace(string(raw))
	}
	var rules []agent.Rule
	if err := db.Order("id").Find(&rules).Error; err != nil {
		t.Fatal(err)
	}
	ruleJSON, _ := json.Marshal(rules)
	publicConfig := func(c agent.Config) map[string]any {
		return map[string]any{"enabled": c.Enabled, "base_url": c.BaseURL, "model": c.Model,
			"input_price": c.InputPriceMicro, "output_price": c.OutputPriceMicro, "price_version": c.PriceVersion,
			"fixed_metrics": c.FixedMetrics, "prefer_fallback": c.MetricSelectPreferFallback,
			"monthly_budget": c.MonthlyBudgetMicro, "run_budget": c.RunBudgetMicro,
			"max_calls": c.MaxCalls, "max_tools": c.MaxTools, "max_output_tokens": c.MaxOutputTokens, "timeout": c.Timeout.String()}
	}
	config := publicConfig(cfg)
	if cfg.Fallback != nil {
		config["fallback"] = publicConfig(*cfg.Fallback)
	}
	configJSON, _ := json.Marshal(config)
	return map[string]string{"code_revision": revision, "binary_sha256": hex.EncodeToString(h.Sum(nil)), "frontend_sha256": hex.EncodeToString(frontend.Sum(nil)), "rules_sha256": digest(ruleJSON), "model_config_sha256": digest(configJSON)}
}

func TestLiveCandidateHashesTheServedFrontendAndModelLimits(t *testing.T) {
	var db *gorm.DB
	_ = newTestRouterWithOptions(t, testRouterOptions{agentFixtureDB: &db})
	root := t.TempDir()
	index := filepath.Join(root, "index.html")
	if err := os.WriteFile(index, []byte("first build"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := agent.DefaultConfig()
	first := liveCandidateIdentity(t, db, cfg, root)
	if len(first["frontend_sha256"]) != 64 {
		t.Fatal(first)
	}
	if err := os.WriteFile(index, []byte("second build"), 0600); err != nil {
		t.Fatal(err)
	}
	second := liveCandidateIdentity(t, db, cfg, root)
	if first["frontend_sha256"] == second["frontend_sha256"] {
		t.Fatal("served build changed without changing identity")
	}
	cfg.MaxOutputTokens++
	third := liveCandidateIdentity(t, db, cfg, root)
	if second["model_config_sha256"] == third["model_config_sha256"] {
		t.Fatal("model limits changed without changing identity")
	}
}
