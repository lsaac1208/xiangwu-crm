package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/lsaac128/kxone/internal/agent"
	"github.com/lsaac128/kxone/internal/security"
)

// Test-only, operator-approved, encrypted backup profile. No plaintext
// credential is accepted in profile files, metadata or command arguments.
func loadLiveFallback(t *testing.T, cfg agent.Config) (agent.Config, string, string) {
	t.Helper()
	path := os.Getenv("KXONE_AGENT_LIVE_FALLBACK_PROFILE")
	if path == "" {
		return cfg, "", ""
	}
	info, err := os.Stat(path)
	if err != nil || !filepath.IsAbs(path) || !info.Mode().IsRegular() || info.Mode().Perm()&0027 != 0 || info.Size() > 16384 {
		t.Fatal("backup profile must be a protected absolute regular file")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("backup profile unavailable")
	}
	var p struct {
		Enabled          bool   `json:"enabled"`
		BaseURL          string `json:"base_url"`
		Model            string `json:"model"`
		KeyCiphertext    string `json:"key_ciphertext"`
		InputPriceMicro  int64  `json:"input_price_micro"`
		OutputPriceMicro int64  `json:"output_price_micro"`
		PriceVersion     string `json:"price_version"`
	}
	if json.Unmarshal(raw, &p) != nil || !p.Enabled || !security.IsEncryptedField(p.KeyCiphertext) || p.Model == "" || p.BaseURL == "" || p.InputPriceMicro <= 0 || p.OutputPriceMicro <= 0 || p.PriceVersion == "" {
		t.Fatal("backup profile incomplete")
	}
	key := os.Getenv("FIELD_ENCRYPTION_KEY")
	if key == "" {
		key = os.Getenv("PII_ENCRYPTION_KEY")
	}
	protector, err := security.NewAESGCMFieldProtector(key)
	if err != nil {
		t.Fatal("backup protector unavailable")
	}
	credential, err := protector.Reveal(p.KeyCiphertext)
	if err != nil || credential == "" {
		t.Fatal("backup credential cannot be decrypted")
	}
	backup := cfg
	backup.Fallback = nil
	backup.BaseURL, backup.Model, backup.APIKey = p.BaseURL, p.Model, credential
	backup.InputPriceMicro, backup.OutputPriceMicro, backup.PriceVersion = p.InputPriceMicro, p.OutputPriceMicro, p.PriceVersion
	cfg.Fallback = &backup
	target := os.Getenv("KXONE_AGENT_LIVE_PROVIDER_TARGET")
	if target == "fallback" {
		cfg = backup
	} else if target != "" && target != "primary" {
		t.Fatal("unknown provider target")
	}
	digest := sha256.Sum256(raw)
	return cfg, path, hex.EncodeToString(digest[:])
}

func liveFallbackModel(cfg agent.Config) string {
	if cfg.Fallback != nil {
		return cfg.Fallback.Model
	}
	return ""
}
