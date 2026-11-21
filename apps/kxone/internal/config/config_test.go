package config

import (
	"strings"
	"testing"
)

func TestValidateProductionSecretsRejectsMissingRequiredSecrets(t *testing.T) {
	base := Config{
		Server: ServerConfig{
			Env:           "prod",
			PublicBaseURL: "https://kxone.example.com",
		},
		JWT: JWTConfig{Secret: "jwt-secret"},
		Database: DatabaseConfig{
			Driver:   "sqlite",
			Database: "/opt/kxone/data/kxone.db",
		},
		AdLead: AdLeadConfig{
			DefaultManagerUserID: "manager-1",
			WebhookSecret:        "lead-secret",
		},
		Security: SecurityConfig{
			FieldEncryptionKey: "field-key",
			MobileHashKey:      "mobile-key",
		},
	}

	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name: "missing public base url",
			mutate: func(cfg *Config) {
				cfg.Server.PublicBaseURL = ""
			},
			wantErr: "SERVER_PUBLIC_BASE_URL",
		},
		{
			name: "default jwt secret",
			mutate: func(cfg *Config) {
				cfg.JWT.Secret = "change-me-in-production"
			},
			wantErr: "JWT_SECRET",
		},
		{
			name: "missing field encryption key",
			mutate: func(cfg *Config) {
				cfg.Security.FieldEncryptionKey = ""
			},
			wantErr: "FIELD_ENCRYPTION_KEY",
		},
		{
			name: "missing mobile hash key",
			mutate: func(cfg *Config) {
				cfg.Security.MobileHashKey = ""
			},
			wantErr: "MOBILE_HASH_KEY",
		},
		{
			name: "missing default lead manager",
			mutate: func(cfg *Config) {
				cfg.AdLead.DefaultManagerUserID = ""
			},
			wantErr: "AD_LEAD_DEFAULT_MANAGER_USERID",
		},
		{
			name: "enabled gateway without hmac secret",
			mutate: func(cfg *Config) {
				cfg.AdLead.GatewayEnabled = true
				cfg.AdLead.GatewayHMACSecret = ""
			},
			wantErr: "AD_LEAD_GATEWAY_HMAC_SECRET",
		},
		{
			name: "relative sqlite path",
			mutate: func(cfg *Config) {
				cfg.Database.Database = "kxone.db"
			},
			wantErr: "DB_NAME",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base
			tt.mutate(&cfg)

			err := cfg.ValidateProductionSecrets()
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestValidateProductionSecretsAllowsDevDefaults(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{Env: "dev"},
		JWT:    JWTConfig{Secret: "change-me-in-production"},
	}

	if err := cfg.ValidateProductionSecrets(); err != nil {
		t.Fatalf("dev config should not require production secrets: %v", err)
	}
}

func TestMobileHashKeyOrFallback(t *testing.T) {
	cfg := &Config{Security: SecurityConfig{
		FieldEncryptionKey: "field-key",
		MobileHashKey:      "mobile-key",
	}}
	if got := cfg.MobileHashKeyOrFallback(); got != "mobile-key" {
		t.Fatalf("MobileHashKeyOrFallback() = %q, want mobile-key", got)
	}

	cfg.Security.MobileHashKey = " "
	if got := cfg.MobileHashKeyOrFallback(); got != "field-key" {
		t.Fatalf("MobileHashKeyOrFallback() fallback = %q, want field-key", got)
	}
}
