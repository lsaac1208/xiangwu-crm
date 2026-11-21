package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/adapter/wecom"
	"github.com/lsaac128/kxone/internal/config"
	"github.com/lsaac128/kxone/internal/security"
)

func main() {
	release := flag.String("release", "", "deployed immutable release identifier")
	flag.Parse()
	if strings.TrimSpace(*release) == "" {
		log.Fatal("--release is required")
	}

	cfg := config.Load()
	if err := cfg.ValidateProductionSecrets(); err != nil {
		log.Fatalf("invalid production configuration: %v", err)
	}
	db, err := repo.NewDB(&cfg.Database)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	protector, err := security.BuildFieldProtector(cfg.Server.Env, cfg.Security.FieldEncryptionKey)
	if err != nil {
		log.Fatalf("prepare field encryption: %v", err)
	}
	settings := repo.NewGormSystemSettingRepository(db).WithFieldProtector(protector)
	sender := wecom.NewDatabaseGroupRobotNotifier(settings)

	chinaStandardTime := time.FixedZone("Asia/Shanghai", 8*60*60)
	content := fmt.Sprintf(
		"【CRM 上线验证】\nRelease：%s\n结果：企业微信群机器人通知链路验证成功\n时间：%s",
		strings.TrimSpace(*release), time.Now().In(chinaStandardTime).Format("2006-01-02 15:04:05"),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := sender.Send(ctx, wecom.GroupRobotMessage{Content: content}); err != nil {
		log.Fatalf("send group robot verification message: %v", err)
	}
	fmt.Println("group robot verification message sent")
}
