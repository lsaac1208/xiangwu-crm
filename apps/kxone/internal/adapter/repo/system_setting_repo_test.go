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

func TestGormSystemSettingRepositoryEncryptsSensitiveValuesAtRest(t *testing.T) {
	db, err := NewDB(&config.DatabaseConfig{Driver: "sqlite", Database: filepath.Join(t.TempDir(), "settings.db"), MaxConns: 4})
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	protector, err := security.NewAESGCMFieldProtector("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("new protector: %v", err)
	}
	repository := NewGormSystemSettingRepository(db).WithFieldProtector(protector)
	now := time.Now()
	setting := &types.SystemSetting{
		Key: "wecom.secret", Value: "plaintext-secret", ValueType: "string",
		Group: "wecom", IsSensitive: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := repository.Upsert(context.Background(), setting); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	var stored SystemSettingModel
	if err := db.Where("setting_key = ?", setting.Key).First(&stored).Error; err != nil {
		t.Fatalf("read stored setting: %v", err)
	}
	if stored.Value == setting.Value || !strings.HasPrefix(stored.Value, "enc:v1:") {
		t.Fatalf("sensitive value is not encrypted at rest: %q", stored.Value)
	}
	loaded, err := repository.GetByKey(context.Background(), setting.Key)
	if err != nil {
		t.Fatalf("get setting: %v", err)
	}
	if loaded == nil || loaded.Value != setting.Value {
		t.Fatalf("loaded setting = %+v, want plaintext value", loaded)
	}
}

func TestGormSystemSettingRepositoryCreateIfAbsentKeepsFirstValue(t *testing.T) {
	db, err := NewDB(&config.DatabaseConfig{Driver: "sqlite", Database: filepath.Join(t.TempDir(), "settings-create.db"), MaxConns: 4})
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repository := NewGormSystemSettingRepository(db)
	now := time.Now().UTC()
	first := &types.SystemSetting{
		Key: "notification.group_robot_rules_rollout_at", Value: now.Format(time.RFC3339Nano),
		ValueType: "datetime", Group: "internal", CreatedAt: now, UpdatedAt: now,
	}
	created, err := repository.CreateIfAbsent(context.Background(), first)
	if err != nil || !created {
		t.Fatalf("first create: created=%v err=%v", created, err)
	}
	second := &types.SystemSetting{
		Key: first.Key, Value: now.Add(time.Hour).Format(time.RFC3339Nano),
		ValueType: "datetime", Group: "internal", CreatedAt: now.Add(time.Hour), UpdatedAt: now.Add(time.Hour),
	}
	created, err = repository.CreateIfAbsent(context.Background(), second)
	if err != nil || created {
		t.Fatalf("second create: created=%v err=%v", created, err)
	}
	stored, err := repository.GetByKey(context.Background(), first.Key)
	if err != nil || stored == nil || stored.Value != first.Value {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
}
