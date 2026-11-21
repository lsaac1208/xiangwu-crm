package usecase

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/config"
	"github.com/lsaac128/kxone/internal/security"
)

func TestChannelIntegrationsCreatesCanonicalCallback(t *testing.T) {
	db, err := repo.NewDB(&config.DatabaseConfig{Driver: "sqlite", Database: filepath.Join(t.TempDir(), "channels.db"), MaxConns: 1})
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	if err := repo.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	protector, err := security.NewAESGCMFieldProtector("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("protector: %v", err)
	}
	uc := NewChannelIntegrationsUseCase(repo.NewGormChannelIntegrationRepository(db).WithFieldProtector(protector), "https://leads.whxiangwu.com/")
	item, err := uc.Create(context.Background(), &SaveChannelIntegrationRequest{Platform: "tencent", AccountName: "武汉明壹致行教育科技有限公司", ActorUserID: "admin"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	callback := uc.CallbackURL(item)
	if !strings.HasPrefix(callback, "https://leads.whxiangwu.com/api/leads/tencent?account=") || strings.Contains(callback, item.AccountName) {
		t.Fatalf("callback = %q", callback)
	}
	result, err := uc.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(result.Integrations) != 1 || len(result.FixedEntries) != 2 {
		t.Fatalf("result = %+v", result)
	}

	douyinCompatible, err := uc.Create(context.Background(), &SaveChannelIntegrationRequest{Platform: "douyin", AccountName: "襄五抖音", ActorUserID: "admin"})
	if err != nil {
		t.Fatalf("create douyin compatible: %v", err)
	}
	if douyinCompatible.CallbackPath != "/api/leads/douyin" || douyinCompatible.AuthMode != "platform_compatible" || douyinCompatible.SecretConfigured {
		t.Fatalf("douyin compatible = %+v", douyinCompatible)
	}

	douyinSigned, err := uc.Create(context.Background(), &SaveChannelIntegrationRequest{Platform: "douyin", AccountName: "襄武抖音", Token: "feiyu-secret", ActorUserID: "admin"})
	if err != nil {
		t.Fatalf("create douyin signed: %v", err)
	}
	if douyinSigned.CallbackPath != "/api/leads/douyin" || douyinSigned.AuthMode != "feiyu_signature" || !douyinSigned.SecretConfigured {
		t.Fatalf("douyin signed = %+v", douyinSigned)
	}
}
