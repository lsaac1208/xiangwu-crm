package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/runtime"
	"github.com/lsaac128/kxone/pkg/types"
	"gorm.io/gorm"
)

type memorySystemSettingRepo struct {
	settings map[string]*types.SystemSetting
}

type notificationSuppressionRecorder struct {
	repo.GroupRobotNotificationRepository
	categories []string
}

func (r *notificationSuppressionRecorder) SuppressDueCategory(_ context.Context, category string, _ time.Time, _ string) (int64, error) {
	r.categories = append(r.categories, category)
	return 1, nil
}

type reminderSuppressionRecorder struct {
	repo.LeadReminderRepository
	types []string
}

type failNthSystemSettingRepository struct {
	repo.SystemSettingRepository
	failAt int
	calls  int
}

func (r *failNthSystemSettingRepository) Upsert(ctx context.Context, setting *types.SystemSetting) error {
	r.calls++
	if r.calls == r.failAt {
		return errors.New("forced setting upsert failure")
	}
	return r.SystemSettingRepository.Upsert(ctx, setting)
}

type adminSettingsTransactionRepositories struct {
	repo.TransactionRepositories
	settings      repo.SystemSettingRepository
	notifications repo.GroupRobotNotificationRepository
	reminders     repo.LeadReminderRepository
}

func (r *adminSettingsTransactionRepositories) SystemSettingRepository() repo.SystemSettingRepository {
	return r.settings
}

func (r *adminSettingsTransactionRepositories) GroupRobotNotificationRepository() repo.GroupRobotNotificationRepository {
	return r.notifications
}

func (r *adminSettingsTransactionRepositories) LeadReminderRepository() repo.LeadReminderRepository {
	return r.reminders
}

type failingAdminSettingsTransactionManager struct {
	db     *gorm.DB
	failAt int
}

func (m *failingAdminSettingsTransactionManager) WithinTransaction(ctx context.Context, fn func(context.Context, repo.TransactionRepositories) error) error {
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		settings := &failNthSystemSettingRepository{
			SystemSettingRepository: repo.NewGormSystemSettingRepository(tx),
			failAt:                  m.failAt,
		}
		return fn(ctx, &adminSettingsTransactionRepositories{
			settings:      settings,
			notifications: repo.NewGormGroupRobotNotificationRepository(tx),
			reminders:     repo.NewGormLeadReminderRepository(tx),
		})
	})
}

func (r *reminderSuppressionRecorder) SuppressDue(_ context.Context, reminderType string, _ time.Time, _ string) (int64, error) {
	r.types = append(r.types, reminderType)
	return 1, nil
}

func newMemorySystemSettingRepo() *memorySystemSettingRepo {
	return &memorySystemSettingRepo{settings: map[string]*types.SystemSetting{}}
}

func (r *memorySystemSettingRepo) List(ctx context.Context, group string) ([]*types.SystemSetting, error) {
	return nil, nil
}

func (r *memorySystemSettingRepo) GetByKey(ctx context.Context, key string) (*types.SystemSetting, error) {
	setting := r.settings[key]
	if setting == nil {
		return nil, nil
	}
	copySetting := *setting
	return &copySetting, nil
}

func (r *memorySystemSettingRepo) Upsert(ctx context.Context, setting *types.SystemSetting) error {
	copySetting := *setting
	if copySetting.ID == 0 {
		copySetting.ID = int64(len(r.settings) + 1)
	}
	if copySetting.CreatedAt.IsZero() {
		copySetting.CreatedAt = time.Now()
	}
	r.settings[copySetting.Key] = &copySetting
	return nil
}

func TestAdminSettingsUpdateAllowsClearingReminderRules(t *testing.T) {
	settingRepo := newMemorySystemSettingRepo()
	settingRepo.settings[runtime.SettingDispatchReminderMins] = &types.SystemSetting{
		ID:        1,
		Key:       runtime.SettingDispatchReminderMins,
		Value:     "3,5,10",
		CreatedAt: time.Now(),
	}

	uc := NewAdminSettingsUseCase(settingRepo)
	err := uc.Update(context.Background(), &UpdateSettingsRequest{
		UpdatedBy: "admin",
		Settings: []UpdateSettingInput{{
			Key:       runtime.SettingDispatchReminderMins,
			Value:     "",
			ValueType: "csv",
			Group:     "business",
		}},
	})
	if err != nil {
		t.Fatalf("update settings: %v", err)
	}
	if got := settingRepo.settings[runtime.SettingDispatchReminderMins].Value; got != "" {
		t.Fatalf("dispatch reminder value = %q, want empty", got)
	}
}

func TestAdminSettingsUpdateKeepsExistingSensitiveValueWhenBlank(t *testing.T) {
	settingRepo := newMemorySystemSettingRepo()
	settingRepo.settings[runtime.SettingWeComSecret] = &types.SystemSetting{
		ID:          1,
		Key:         runtime.SettingWeComSecret,
		Value:       "old-secret",
		IsSensitive: true,
		CreatedAt:   time.Now(),
	}

	uc := NewAdminSettingsUseCase(settingRepo)
	err := uc.Update(context.Background(), &UpdateSettingsRequest{
		UpdatedBy: "admin",
		Settings: []UpdateSettingInput{{
			Key:         runtime.SettingWeComSecret,
			Value:       "",
			ValueType:   "string",
			Group:       "wecom",
			IsSensitive: true,
		}},
	})
	if err != nil {
		t.Fatalf("update settings: %v", err)
	}
	if got := settingRepo.settings[runtime.SettingWeComSecret].Value; got != "old-secret" {
		t.Fatalf("secret value = %q, want old-secret", got)
	}
}

func TestAdminSettingsUpdateValidatesReminderCSV(t *testing.T) {
	settingRepo := newMemorySystemSettingRepo()
	uc := NewAdminSettingsUseCase(settingRepo)

	err := uc.Update(context.Background(), &UpdateSettingsRequest{
		UpdatedBy: "admin",
		Settings: []UpdateSettingInput{{
			Key:       runtime.SettingClaimReminderMins,
			Value:     "1, bad",
			ValueType: "csv",
			Group:     "business",
		}},
	})
	if err == nil {
		t.Fatal("expected invalid csv error")
	}

	err = uc.Update(context.Background(), &UpdateSettingsRequest{
		UpdatedBy: "admin",
		Settings: []UpdateSettingInput{{
			Key:       runtime.SettingClaimReminderMins,
			Value:     "1, 2, 5",
			ValueType: "csv",
			Group:     "business",
		}},
	})
	if err != nil {
		t.Fatalf("update valid csv: %v", err)
	}
	if got := settingRepo.settings[runtime.SettingClaimReminderMins].Value; got != "1,2,5" {
		t.Fatalf("claim reminder value = %q, want 1,2,5", got)
	}
}

func TestAdminSettingsUpdateTreatsZeroReminderCSVAsDisabled(t *testing.T) {
	settingRepo := newMemorySystemSettingRepo()
	uc := NewAdminSettingsUseCase(settingRepo)

	err := uc.Update(context.Background(), &UpdateSettingsRequest{
		UpdatedBy: "admin",
		Settings: []UpdateSettingInput{{
			Key:       runtime.SettingDispatchReminderMins,
			Value:     "0",
			ValueType: "csv",
			Group:     "business",
		}},
	})
	if err != nil {
		t.Fatalf("update zero csv: %v", err)
	}
	if got := settingRepo.settings[runtime.SettingDispatchReminderMins].Value; got != "" {
		t.Fatalf("dispatch reminder value = %q, want empty", got)
	}
}

func TestAdminSettingsUpdateValidatesGroupRobotSettings(t *testing.T) {
	settingRepo := newMemorySystemSettingRepo()
	uc := NewAdminSettingsUseCase(settingRepo)

	err := uc.Update(context.Background(), &UpdateSettingsRequest{Settings: []UpdateSettingInput{
		{Key: runtime.SettingGroupRobotEnabled, Value: "yes"},
	}})
	if err == nil {
		t.Fatal("expected invalid group robot switch error")
	}

	err = uc.Update(context.Background(), &UpdateSettingsRequest{Settings: []UpdateSettingInput{
		{Key: runtime.SettingGroupRobotWebhookURL, Value: "https://example.com/hook", IsSensitive: true},
	}})
	if err == nil {
		t.Fatal("expected invalid group robot webhook error")
	}

	err = uc.Update(context.Background(), &UpdateSettingsRequest{Settings: []UpdateSettingInput{
		{Key: runtime.SettingGroupRobotEnabled, Value: "true", ValueType: "boolean"},
		{Key: runtime.SettingGroupRobotWebhookURL, Value: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=test", IsSensitive: true},
	}})
	if err != nil {
		t.Fatalf("update valid group robot settings: %v", err)
	}
}

func TestAdminSettingsUpdateValidatesWorkflowFeatureFlags(t *testing.T) {
	settingRepo := newMemorySystemSettingRepo()
	uc := NewAdminSettingsUseCase(settingRepo)

	err := uc.Update(context.Background(), &UpdateSettingsRequest{Settings: []UpdateSettingInput{
		{Key: runtime.SettingWorkflowV2Enabled, Value: "yes"},
	}})
	if err == nil {
		t.Fatal("expected invalid workflow feature flag error")
	}

	err = uc.Update(context.Background(), &UpdateSettingsRequest{Settings: []UpdateSettingInput{
		{Key: runtime.SettingWorkflowV2Enabled, Value: "false", ValueType: "boolean", Group: "workflow"},
		{Key: runtime.SettingAppointmentEnabled, Value: "false", ValueType: "boolean", Group: "workflow"},
		{Key: runtime.SettingPoolEnabled, Value: "false", ValueType: "boolean", Group: "workflow"},
		{Key: runtime.SettingPoolAutoReturnEnabled, Value: "false", ValueType: "boolean", Group: "workflow"},
	}})
	if err != nil {
		t.Fatalf("update workflow feature flags: %v", err)
	}
	for _, key := range []string{
		runtime.SettingWorkflowV2Enabled,
		runtime.SettingAppointmentEnabled,
		runtime.SettingPoolEnabled,
		runtime.SettingPoolAutoReturnEnabled,
	} {
		if got := settingRepo.settings[key].Value; got != "false" {
			t.Fatalf("%s = %q, want false", key, got)
		}
	}
}

func TestAdminSettingsPublicPoolDependencies(t *testing.T) {
	settingRepo := newMemorySystemSettingRepo()
	uc := NewAdminSettingsUseCase(settingRepo)

	if err := uc.Update(context.Background(), &UpdateSettingsRequest{Settings: []UpdateSettingInput{
		{Key: runtime.SettingPoolAutoReturnEnabled, Value: "true", ValueType: "boolean", Group: "workflow"},
	}}); err == nil {
		t.Fatal("expected auto return to require the public pool")
	}

	if err := uc.Update(context.Background(), &UpdateSettingsRequest{Settings: []UpdateSettingInput{
		{Key: runtime.SettingPoolEnabled, Value: "true", ValueType: "boolean", Group: "workflow"},
		{Key: runtime.SettingPoolAutoReturnEnabled, Value: "true", ValueType: "boolean", Group: "workflow"},
	}}); err != nil {
		t.Fatalf("enable public pool and auto return: %v", err)
	}
	if err := uc.Update(context.Background(), &UpdateSettingsRequest{Settings: []UpdateSettingInput{
		{Key: runtime.SettingPoolEnabled, Value: "false", ValueType: "boolean", Group: "workflow"},
	}}); err != nil {
		t.Fatalf("disable public pool: %v", err)
	}
	if settingRepo.settings[runtime.SettingPoolAutoReturnEnabled].Value != "false" {
		t.Fatal("disabling the public pool must also disable auto return")
	}
}

func TestAdminSettingsUpdateValidatesStage3Policies(t *testing.T) {
	settingRepo := newMemorySystemSettingRepo()
	uc := NewAdminSettingsUseCase(settingRepo)
	if err := uc.Update(context.Background(), &UpdateSettingsRequest{Settings: []UpdateSettingInput{{Key: runtime.SettingFirstFollowupDueMins, Value: "0"}}}); err == nil {
		t.Fatal("expected non-positive first follow-up rule to fail")
	}
	if err := uc.Update(context.Background(), &UpdateSettingsRequest{Settings: []UpdateSettingInput{{Key: runtime.SettingFollowupReminderMins, Value: "0,-1"}}}); err == nil {
		t.Fatal("expected negative reminder offset to fail")
	}
	err := uc.Update(context.Background(), &UpdateSettingsRequest{Settings: []UpdateSettingInput{
		{Key: runtime.SettingFirstFollowupDueMins, Value: "90", ValueType: "number", Group: "workflow"},
		{Key: runtime.SettingFollowupReminderMins, Value: "0, 60, 240", ValueType: "csv_int", Group: "workflow"},
		{Key: runtime.SettingPoolMaxActiveLeads, Value: "50", ValueType: "number", Group: "workflow"},
	}})
	if err != nil {
		t.Fatalf("update stage3 policies: %v", err)
	}
	if settingRepo.settings[runtime.SettingFollowupReminderMins].Value != "0,60,240" {
		t.Fatalf("unexpected normalized offsets")
	}
}

func TestAdminSettingsInvalidWebhookDoesNotPartiallyPersist(t *testing.T) {
	settingRepo := newMemorySystemSettingRepo()
	uc := NewAdminSettingsUseCase(settingRepo)
	err := uc.Update(context.Background(), &UpdateSettingsRequest{Settings: []UpdateSettingInput{
		{Key: runtime.SettingGroupRobotEnabled, Value: "false", ValueType: "boolean"},
		{Key: runtime.SettingGroupRobotWebhookURL, Value: "https://example.com/not-wecom", IsSensitive: false},
	}})
	if err == nil {
		t.Fatal("expected invalid webhook")
	}
	if len(settingRepo.settings) != 0 {
		t.Fatalf("validation failure partially persisted settings: %+v", settingRepo.settings)
	}
}

func TestAdminSettingsCannotClearWebhookWhileNotificationEnabled(t *testing.T) {
	settingRepo := newMemorySystemSettingRepo()
	settingRepo.settings[runtime.SettingGroupRobotEnabled] = &types.SystemSetting{
		ID: 1, Key: runtime.SettingGroupRobotEnabled, Value: "true", ValueType: "boolean", CreatedAt: time.Now(),
	}
	const webhook = "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=existing"
	settingRepo.settings[runtime.SettingGroupRobotWebhookURL] = &types.SystemSetting{
		ID: 2, Key: runtime.SettingGroupRobotWebhookURL, Value: webhook, IsSensitive: true, CreatedAt: time.Now(),
	}
	uc := NewAdminSettingsUseCase(settingRepo)
	err := uc.Update(context.Background(), &UpdateSettingsRequest{Settings: []UpdateSettingInput{
		{Key: runtime.SettingGroupRobotWebhookURL, Value: "", IsSensitive: false},
	}})
	if err == nil {
		t.Fatal("expected enabled notification to reject webhook clearing")
	}
	if got := settingRepo.settings[runtime.SettingGroupRobotWebhookURL].Value; got != webhook {
		t.Fatalf("webhook changed after rejected update: %q", got)
	}
}

func TestAdminSettingsForcesWebhookSensitiveAndSuppressesDueFollowupsOnEnable(t *testing.T) {
	settingRepo := newMemorySystemSettingRepo()
	notifications := &notificationSuppressionRecorder{}
	reminders := &reminderSuppressionRecorder{}
	uc := NewAdminSettingsUseCase(settingRepo).
		WithGroupRobotNotifications(notifications).
		WithLeadReminders(reminders)
	const webhook = "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=new"
	err := uc.Update(context.Background(), &UpdateSettingsRequest{Settings: []UpdateSettingInput{
		{Key: runtime.SettingFollowupNotify, Value: "true", ValueType: "boolean"},
		{Key: runtime.SettingGroupRobotWebhookURL, Value: webhook, IsSensitive: false},
	}})
	if err != nil {
		t.Fatal(err)
	}
	stored := settingRepo.settings[runtime.SettingGroupRobotWebhookURL]
	if stored == nil || !stored.IsSensitive || stored.Value != webhook {
		t.Fatalf("webhook sensitivity was not enforced: %+v", stored)
	}
	if len(notifications.categories) != 1 || notifications.categories[0] != types.GroupRobotCategoryFollowup {
		t.Fatalf("notification suppressions = %+v", notifications.categories)
	}
	if len(reminders.types) != 1 || reminders.types[0] != types.ReminderTypeFollowupDue {
		t.Fatalf("reminder suppressions = %+v", reminders.types)
	}
}

func TestAdminSettingsTransactionRollsBackSuppressionsAndEarlierUpserts(t *testing.T) {
	db := newStage3TestDB(t)
	settings := repo.NewGormSystemSettingRepository(db)
	notifications := repo.NewGormGroupRobotNotificationRepository(db)
	reminders := repo.NewGormLeadReminderRepository(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	if err := settings.Upsert(ctx, &types.SystemSetting{
		Key: runtime.SettingGroupRobotWebhookURL, Value: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=atomic",
		ValueType: "string", Group: "notification", IsSensitive: true, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		key      string
		category string
	}{
		{key: "atomic-new", category: types.GroupRobotCategoryNewLead},
		{key: "atomic-followup", category: types.GroupRobotCategoryFollowup},
	} {
		if _, err := notifications.Create(ctx, &types.GroupRobotNotification{
			EventKey: item.key, Category: item.category, EventType: types.GroupRobotEventNewLead,
			LeadID: 1, DueAt: now.Add(-time.Minute), Status: types.GroupRobotNotificationPending,
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	reminder := &types.LeadReminderRecord{
		LeadID: 1, ReminderType: types.ReminderTypeFollowupDue, DueAt: now.Add(-time.Minute),
		ReceiverUserID: "inviter-1", Status: types.ReminderStatusPending,
		IdempotencyKey: "atomic-followup-reminder", CreatedAt: now, UpdatedAt: now,
	}
	if _, err := reminders.Create(ctx, reminder); err != nil {
		t.Fatal(err)
	}

	useCase := NewAdminSettingsUseCase(settings).
		WithGroupRobotNotifications(notifications).
		WithLeadReminders(reminders).
		WithTransactionManager(&failingAdminSettingsTransactionManager{db: db, failAt: 2})
	err := useCase.Update(ctx, &UpdateSettingsRequest{Settings: []UpdateSettingInput{
		{Key: runtime.SettingGroupRobotEnabled, Value: "true", ValueType: "boolean", Group: "notification"},
		{Key: runtime.SettingFollowupNotify, Value: "true", ValueType: "boolean", Group: "notification"},
	}})
	if err == nil || !strings.Contains(err.Error(), "forced setting upsert failure") {
		t.Fatalf("update error = %v", err)
	}
	for _, key := range []string{runtime.SettingGroupRobotEnabled, runtime.SettingFollowupNotify} {
		stored, queryErr := settings.GetByKey(ctx, key)
		if queryErr != nil || stored != nil {
			t.Fatalf("setting %s survived rollback: %+v err=%v", key, stored, queryErr)
		}
	}
	for _, eventKey := range []string{"atomic-new", "atomic-followup"} {
		record, queryErr := notifications.GetByEventKey(ctx, eventKey)
		if queryErr != nil || record.Status != types.GroupRobotNotificationPending {
			t.Fatalf("notification %s was not rolled back: %+v err=%v", eventKey, record, queryErr)
		}
	}
	var storedReminder repo.LeadReminderRecordModel
	if err := db.First(&storedReminder, reminder.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedReminder.Status != types.ReminderStatusPending {
		t.Fatalf("reminder suppression survived rollback: %s", storedReminder.Status)
	}
}
