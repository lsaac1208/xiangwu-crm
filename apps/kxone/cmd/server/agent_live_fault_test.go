package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"github.com/lsaac128/kxone/internal/agent"
)

// This opt-in fault exists only in the isolated test binary. It never proxies
// credentials and never replaces the real backup provider's responses.
func livePrimaryFault(t *testing.T, cfg agent.Config) (agent.Config, map[string]any, *atomic.Int64) {
	t.Helper()
	mode := os.Getenv("KXONE_AGENT_LIVE_PRIMARY_FAULT")
	if mode == "" {
		return cfg, nil, nil
	}
	if mode != "http_503" || cfg.Fallback == nil || os.Getenv("KXONE_AGENT_LIVE_PROVIDER_TARGET") != "primary" {
		t.Fatal("primary fault requires explicit http_503 mode and a real backup profile")
	}
	count := &atomic.Int64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer isolated-fault-no-cloud-credential" {
			http.Error(w, "unexpected isolated fault request", http.StatusBadRequest)
			return
		}
		count.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "isolated_primary_fault", "message": "Explicit synthetic primary failure; backup remains real"}})
	}))
	t.Cleanup(server.Close)
	cfg.BaseURL, cfg.APIKey = server.URL, "isolated-fault-no-cloud-credential"
	return cfg, map[string]any{"kind": "primary_http_503", "scope": "all_primary_calls_in_isolated_fixture", "synthetic_primary_failure": true, "real_fallback": true}, count
}

func TestLivePrimaryFaultIsExplicitAndDoesNotMutateBackup(t *testing.T) {
	cfg := agent.DefaultConfig()
	cfg.BaseURL, cfg.APIKey = "https://primary.example.invalid/v1", "private-primary"
	backup := cfg
	backup.BaseURL, backup.APIKey, backup.Model = "https://backup.example.invalid/v1", "private-backup", "backup"
	cfg.Fallback = &backup
	t.Setenv("KXONE_AGENT_LIVE_PRIMARY_FAULT", "")
	unchanged, metadata, count := livePrimaryFault(t, cfg)
	if unchanged.BaseURL != cfg.BaseURL || metadata != nil || count != nil {
		t.Fatal("default fixture unexpectedly injects a fault")
	}
	t.Setenv("KXONE_AGENT_LIVE_PRIMARY_FAULT", "http_503")
	t.Setenv("KXONE_AGENT_LIVE_PROVIDER_TARGET", "primary")
	changed, metadata, count := livePrimaryFault(t, cfg)
	if changed.APIKey == cfg.APIKey || changed.Fallback != cfg.Fallback || metadata["real_fallback"] != true {
		t.Fatal("fault altered backup or reused cloud credential")
	}
	request, _ := http.NewRequest(http.MethodPost, changed.BaseURL+"/chat/completions", nil)
	request.Header.Set("Authorization", "Bearer "+changed.APIKey)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable || count.Load() != 1 {
		t.Fatal("explicit fault was not observed")
	}
}
