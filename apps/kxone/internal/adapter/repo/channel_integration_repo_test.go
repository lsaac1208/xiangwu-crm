package repo

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/config"
	"github.com/lsaac128/kxone/internal/security"
	"github.com/lsaac128/kxone/pkg/types"
)

func TestChannelIntegrationRepositoryPersistsAuditAndReceiveStatus(t *testing.T) {
	db, err := NewDB(&config.DatabaseConfig{Driver: "sqlite", Database: filepath.Join(t.TempDir(), "channels.db"), MaxConns: 1})
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repository := NewGormChannelIntegrationRepository(db)
	now := time.Now()
	item := &types.ChannelIntegration{Platform: "baidu", AccountName: "百度地图", CallbackPath: "/api/leads/baidu", AuthMode: "platform_compatible", Enabled: true, CreatedBy: "admin", UpdatedBy: "admin", CreatedAt: now, UpdatedAt: now}
	if err := repository.Create(context.Background(), item); err != nil {
		t.Fatalf("create: %v", err)
	}
	leadID := int64(42)
	if err := repository.RecordReceive(context.Background(), item.ID, "accepted", &leadID, ""); err != nil {
		t.Fatalf("record: %v", err)
	}
	loaded, err := repository.GetByPlatformAccount(context.Background(), "baidu", "百度地图")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if loaded == nil || loaded.LastStatus != "accepted" || loaded.LastLeadID == nil || *loaded.LastLeadID != leadID {
		t.Fatalf("loaded = %+v", loaded)
	}
	var audits int64
	if err := db.Model(&ChannelIntegrationAuditModel{}).Where("integration_id = ? AND action = ?", item.ID, "create").Count(&audits).Error; err != nil {
		t.Fatalf("audit count: %v", err)
	}
	if audits != 1 {
		t.Fatalf("audit count = %d", audits)
	}
}

func TestChannelIntegrationRepositoryEncryptsWebhookToken(t *testing.T) {
	db, err := NewDB(&config.DatabaseConfig{Driver: "sqlite", Database: filepath.Join(t.TempDir(), "channels.db"), MaxConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	protector, err := security.NewAESGCMFieldProtector("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	repository := NewGormChannelIntegrationRepository(db).WithFieldProtector(protector)
	now := time.Now()
	item := &types.ChannelIntegration{Platform: "xiaohongshu", AccountName: "加密账户", CallbackPath: "/api/leads/xiaohongshu", AuthMode: "hmac_sha1", WebhookToken: "plain-webhook-token", Enabled: true, CreatedBy: "admin", UpdatedBy: "admin", CreatedAt: now, UpdatedAt: now}
	if err := repository.Create(t.Context(), item); err != nil {
		t.Fatal(err)
	}
	var stored ChannelIntegrationModel
	if err := db.First(&stored, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.TokenCiphertext == item.WebhookToken || !security.IsEncryptedField(stored.TokenCiphertext) {
		t.Fatalf("token was not encrypted")
	}
	loaded, err := repository.GetByID(t.Context(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.WebhookToken != item.WebhookToken || !loaded.SecretConfigured {
		t.Fatalf("loaded = %+v", loaded)
	}
	var audit ChannelIntegrationAuditModel
	if err := db.Where("integration_id = ?", item.ID).First(&audit).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(audit.SnapshotJSON, item.WebhookToken) {
		t.Fatal("audit contains plaintext token")
	}
}

func TestChannelIntegrationRepositoryDeletesWithAudit(t *testing.T) {
	db, err := NewDB(&config.DatabaseConfig{Driver: "sqlite", Database: filepath.Join(t.TempDir(), "channels.db"), MaxConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repository := NewGormChannelIntegrationRepository(db)
	now := time.Now()
	item := &types.ChannelIntegration{Platform: "tencent", AccountName: "待删除账户", CallbackPath: "/api/leads/tencent", AuthMode: "platform_compatible", Enabled: true, CreatedBy: "admin", UpdatedBy: "admin", CreatedAt: now, UpdatedAt: now}
	if err := repository.Create(t.Context(), item); err != nil {
		t.Fatal(err)
	}
	if err := repository.Delete(t.Context(), item.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	loaded, err := repository.GetByID(t.Context(), item.ID)
	if err != nil || loaded != nil {
		t.Fatalf("deleted integration = %+v, err = %v", loaded, err)
	}
	var audits int64
	if err := db.Model(&ChannelIntegrationAuditModel{}).Where("integration_id = ? AND action = ? AND actor_user_id = ?", item.ID, "delete", "admin").Count(&audits).Error; err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("delete audit count = %d", audits)
	}
}
