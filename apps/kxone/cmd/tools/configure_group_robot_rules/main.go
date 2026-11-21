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
	"github.com/lsaac128/kxone/internal/runtime"
	"github.com/lsaac128/kxone/internal/security"
	"github.com/lsaac128/kxone/internal/usecase"
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

	txManager := repo.NewGormTransactionManager(db).WithLeadSecurity(protector, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	settings := []usecase.UpdateSettingInput{
		setting(runtime.SettingDispatchReminderMins, "3,5,10", "list", "reminder", "待分配提醒分钟"),
		setting(runtime.SettingClaimReminderMins, "3,5,10", "list", "reminder", "待领取提醒分钟"),
		setting(runtime.SettingFirstFollowupDueMins, "120", "number", "followup", "首次回访截止分钟"),
		setting(runtime.SettingFollowupReminderMins, "0,60,240", "list", "followup", "回访逾期提醒偏移分钟"),
		setting(runtime.SettingStar1FollowupHours, "168", "number", "followup", "1 星回访周期小时"),
		setting(runtime.SettingStar2FollowupHours, "120", "number", "followup", "2 星回访周期小时"),
		setting(runtime.SettingStar3FollowupHours, "72", "number", "followup", "3 星回访周期小时"),
		setting(runtime.SettingStar4FollowupHours, "48", "number", "followup", "4 星回访周期小时"),
		setting(runtime.SettingStar5FollowupHours, "24", "number", "followup", "5 星回访周期小时"),
		setting(runtime.SettingPoolMaxActiveLeads, "100", "number", "pool", "邀约师私有线索上限"),
		setting(runtime.SettingPoolCooldownMins, "1440", "number", "pool", "原负责人公海领取冷却分钟"),
		setting(runtime.SettingPoolUnclaimedMins, "1440", "number", "pool", "未领取自动回公海分钟"),
		setting(runtime.SettingPoolFollowupGraceMins, "240", "number", "pool", "回访逾期自动回公海宽限分钟"),
		setting(runtime.SettingPoolAutoReturnEnabled, "false", "boolean", "workflow", "自动回公海"),
		setting(runtime.SettingGroupRobotEnabled, "true", "boolean", "notification", "企业微信群新线索通知"),
		setting(runtime.SettingDispatchClaimNotify, "true", "boolean", "notification", "企业微信群派发与领取提醒"),
		setting(runtime.SettingFollowupNotify, "true", "boolean", "notification", "企业微信群回访提醒"),
		setting(runtime.SettingPoolNotify, "true", "boolean", "notification", "企业微信群自动回公海通知"),
	}
	var cutoff time.Time
	err = txManager.WithinTransaction(ctx, func(ctx context.Context, repositories repo.TransactionRepositories) error {
		transactionalSettings := repositories.SystemSettingRepository()
		runtimeSettings := runtime.NewDatabaseRuntimeSettingsWithWeComConfig(transactionalSettings, wecom.Config{})
		var cutoffErr error
		cutoff, cutoffErr = runtimeSettings.EnsureGroupRobotRulesRolloutCutoff(ctx, time.Now())
		if cutoffErr != nil {
			return cutoffErr
		}

		settingsUseCase := usecase.NewAdminSettingsUseCase(transactionalSettings).
			WithGroupRobotNotifications(repositories.GroupRobotNotificationRepository()).
			WithLeadReminders(repositories.LeadReminderRepository())
		return settingsUseCase.Update(ctx, &usecase.UpdateSettingsRequest{
			Settings: settings, UpdatedBy: "system:release:" + strings.TrimSpace(*release),
		})
	})
	if err != nil {
		log.Fatalf("configure group robot rules: %v", err)
	}

	fmt.Printf("group robot rules configured; rollout_cutoff=%s auto_return=false\n", cutoff.Format(time.RFC3339Nano))
}

func setting(key, value, valueType, group, description string) usecase.UpdateSettingInput {
	return usecase.UpdateSettingInput{
		Key: key, Value: value, ValueType: valueType, Group: group, Description: description,
	}
}
