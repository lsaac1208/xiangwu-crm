package usecase

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/runtime"
	"github.com/lsaac128/kxone/pkg/types"
)

// AdminSettingsUseCase 管理系统配置用例。
type AdminSettingsUseCase struct {
	settingRepo      repo.SystemSettingRepository
	notificationRepo repo.GroupRobotNotificationRepository
	reminderRepo     repo.LeadReminderRepository
	txManager        repo.TransactionManager
}

func NewAdminSettingsUseCase(settingRepo repo.SystemSettingRepository) *AdminSettingsUseCase {
	return &AdminSettingsUseCase{settingRepo: settingRepo}
}

func (uc *AdminSettingsUseCase) WithGroupRobotNotifications(repository repo.GroupRobotNotificationRepository) *AdminSettingsUseCase {
	uc.notificationRepo = repository
	return uc
}

func (uc *AdminSettingsUseCase) WithLeadReminders(repository repo.LeadReminderRepository) *AdminSettingsUseCase {
	uc.reminderRepo = repository
	return uc
}

func (uc *AdminSettingsUseCase) WithTransactionManager(manager repo.TransactionManager) *AdminSettingsUseCase {
	uc.txManager = manager
	return uc
}

type ListSettingsRequest struct {
	Group string
}

type ListSettingsResponse struct {
	Settings []*types.SystemSetting `json:"settings"`
}

type UpdateSettingInput struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	ValueType   string `json:"value_type"`
	Group       string `json:"group"`
	Description string `json:"description"`
	IsSensitive bool   `json:"is_sensitive"`
}

type UpdateSettingsRequest struct {
	Settings  []UpdateSettingInput
	UpdatedBy string
}

func (uc *AdminSettingsUseCase) List(ctx context.Context, req *ListSettingsRequest) (*ListSettingsResponse, error) {
	settings, err := uc.settingRepo.List(ctx, req.Group)
	if err != nil {
		return nil, err
	}
	return &ListSettingsResponse{Settings: maskSensitiveSettings(settings)}, nil
}

func (uc *AdminSettingsUseCase) Update(ctx context.Context, req *UpdateSettingsRequest) error {
	if uc.txManager != nil {
		return uc.txManager.WithinTransaction(ctx, func(ctx context.Context, repositories repo.TransactionRepositories) error {
			transactional := *uc
			transactional.settingRepo = repositories.SystemSettingRepository()
			transactional.notificationRepo = repositories.GroupRobotNotificationRepository()
			transactional.reminderRepo = repositories.LeadReminderRepository()
			transactional.txManager = nil
			return transactional.update(ctx, req)
		})
	}
	return uc.update(ctx, req)
}

func (uc *AdminSettingsUseCase) update(ctx context.Context, req *UpdateSettingsRequest) error {
	settings, err := uc.normalizeDependentSettings(ctx, req.Settings)
	if err != nil {
		return err
	}
	settings, err = normalizeSettingInputs(settings)
	if err != nil {
		return err
	}
	if err := uc.validateGroupRobotDependencies(ctx, settings); err != nil {
		return err
	}
	if err := uc.suppressExpiredNotificationsBeforeEnable(ctx, settings); err != nil {
		return err
	}
	now := time.Now()
	for _, input := range settings {
		if input.Key == "" {
			continue
		}

		valueType := input.ValueType
		if valueType == "" {
			valueType = "string"
		}
		group := input.Group
		if group == "" {
			group = "general"
		}

		existing, err := uc.settingRepo.GetByKey(ctx, input.Key)
		if err != nil {
			return err
		}

		setting := &types.SystemSetting{
			Key:         input.Key,
			Value:       input.Value,
			ValueType:   valueType,
			Group:       group,
			Description: input.Description,
			IsSensitive: input.IsSensitive,
			UpdatedBy:   req.UpdatedBy,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if existing != nil {
			setting.ID = existing.ID
			setting.CreatedAt = existing.CreatedAt
			keepExisting := input.IsSensitive && shouldKeepExistingSensitiveValue(input.Value, existing.Value)
			if input.Key == runtime.SettingGroupRobotWebhookURL && strings.TrimSpace(input.Value) == "" {
				keepExisting = false
			}
			if keepExisting {
				setting.Value = existing.Value
			}
		} else if input.Key == runtime.SettingGroupRobotWebhookURL && input.Value == "******" {
			setting.Value = ""
		}

		if err := uc.settingRepo.Upsert(ctx, setting); err != nil {
			return err
		}
	}
	return nil
}

func normalizeSettingInputs(inputs []UpdateSettingInput) ([]UpdateSettingInput, error) {
	result := append([]UpdateSettingInput(nil), inputs...)
	for index := range result {
		if result[index].Key == "" {
			continue
		}
		normalizedValue, err := normalizeSettingValue(result[index])
		if err != nil {
			return nil, err
		}
		result[index].Value = normalizedValue
		if result[index].Key == runtime.SettingGroupRobotWebhookURL {
			result[index].IsSensitive = true
		}
	}
	return result, nil
}

func (uc *AdminSettingsUseCase) normalizeDependentSettings(ctx context.Context, inputs []UpdateSettingInput) ([]UpdateSettingInput, error) {
	result := append([]UpdateSettingInput(nil), inputs...)
	poolIndex, autoIndex := -1, -1
	for index := range result {
		switch result[index].Key {
		case runtime.SettingPoolEnabled:
			poolIndex = index
		case runtime.SettingPoolAutoReturnEnabled:
			autoIndex = index
		}
	}

	poolEnabled := false
	if existing, err := uc.settingRepo.GetByKey(ctx, runtime.SettingPoolEnabled); err != nil {
		return nil, err
	} else if existing != nil {
		poolEnabled = strings.EqualFold(strings.TrimSpace(existing.Value), "true")
	}
	if poolIndex >= 0 {
		poolEnabled = strings.EqualFold(strings.TrimSpace(result[poolIndex].Value), "true")
	}

	if !poolEnabled {
		if poolIndex < 0 && autoIndex >= 0 && strings.EqualFold(strings.TrimSpace(result[autoIndex].Value), "true") {
			return nil, fmt.Errorf("%s requires %s to be enabled", runtime.SettingPoolAutoReturnEnabled, runtime.SettingPoolEnabled)
		}
		if autoIndex >= 0 {
			result[autoIndex].Value = "false"
		} else if poolIndex >= 0 {
			result = append(result, UpdateSettingInput{
				Key: runtime.SettingPoolAutoReturnEnabled, Value: "false", ValueType: "boolean",
				Group: "workflow", Description: "邀约或销售超时自动回公海",
			})
		}
	}
	return result, nil
}

func normalizeSettingValue(input UpdateSettingInput) (string, error) {
	value := strings.TrimSpace(input.Value)
	switch input.Key {
	case runtime.SettingDispatchReminderMins, runtime.SettingClaimReminderMins, runtime.SettingCardRetrySeconds:
		return normalizePositiveIntCSV(input.Key, value)
	case runtime.SettingFollowupReminderMins:
		return normalizeNonNegativeIntCSV(input.Key, value)
	case runtime.SettingFirstFollowupDueMins,
		runtime.SettingStar1FollowupHours,
		runtime.SettingStar2FollowupHours,
		runtime.SettingStar3FollowupHours,
		runtime.SettingStar4FollowupHours,
		runtime.SettingStar5FollowupHours,
		runtime.SettingPoolMaxActiveLeads,
		runtime.SettingPoolCooldownMins,
		runtime.SettingPoolUnclaimedMins,
		runtime.SettingPoolFollowupGraceMins,
		runtime.SettingAppointmentDefaultMins,
		runtime.SettingAppointmentBufferMins,
		runtime.SettingAppointmentDailyLimit,
		runtime.SettingAppointmentWorkStart,
		runtime.SettingAppointmentWorkEnd,
		runtime.SettingAppointmentNoShowMins:
		number, err := strconv.Atoi(value)
		if err != nil || number <= 0 {
			return "", fmt.Errorf("%s must be a positive integer", input.Key)
		}
		return strconv.Itoa(number), nil
	case runtime.SettingGroupRobotEnabled,
		runtime.SettingDispatchClaimNotify,
		runtime.SettingFollowupNotify,
		runtime.SettingPoolNotify,
		runtime.SettingGroupRobotMentionsEnabled,
		runtime.SettingWorkflowV2Enabled,
		runtime.SettingAppointmentEnabled,
		runtime.SettingPoolEnabled,
		runtime.SettingPoolAutoReturnEnabled:
		if value != "true" && value != "false" {
			return "", fmt.Errorf("%s must be true or false", input.Key)
		}
		return value, nil
	case runtime.SettingGroupRobotWebhookURL:
		if value == "" || value == "******" {
			return value, nil
		}
		parsed, err := url.ParseRequestURI(value)
		if err != nil || parsed.Scheme != "https" || parsed.Host != "qyapi.weixin.qq.com" ||
			parsed.Path != "/cgi-bin/webhook/send" || strings.TrimSpace(parsed.Query().Get("key")) == "" {
			return "", fmt.Errorf("%s must be an enterprise WeChat group robot webhook URL", input.Key)
		}
		return value, nil
	default:
		return value, nil
	}
}

func (uc *AdminSettingsUseCase) validateGroupRobotDependencies(ctx context.Context, inputs []UpdateSettingInput) error {
	webhookValue := ""
	if existing, err := uc.settingRepo.GetByKey(ctx, runtime.SettingGroupRobotWebhookURL); err != nil {
		return err
	} else if existing != nil {
		webhookValue = strings.TrimSpace(existing.Value)
	}
	enabled := make(map[string]bool, 4)
	categorySettingKeys := []string{
		runtime.SettingGroupRobotEnabled,
		runtime.SettingDispatchClaimNotify,
		runtime.SettingFollowupNotify,
		runtime.SettingPoolNotify,
	}
	for _, key := range categorySettingKeys {
		existing, err := uc.settingRepo.GetByKey(ctx, key)
		if err != nil {
			return err
		}
		enabled[key] = existing != nil && strings.EqualFold(strings.TrimSpace(existing.Value), "true")
	}
	for _, input := range inputs {
		if _, ok := groupRobotCategoryForSetting(input.Key); ok {
			enabled[input.Key] = input.Value == "true"
		}
		if input.Key == runtime.SettingGroupRobotWebhookURL && input.Value != "******" {
			webhookValue = strings.TrimSpace(input.Value)
		}
	}
	for _, key := range categorySettingKeys {
		if !enabled[key] {
			continue
		}
		if webhookValue == "" {
			return fmt.Errorf("%s requires %s to be configured", key, runtime.SettingGroupRobotWebhookURL)
		}
		if _, err := normalizeSettingValue(UpdateSettingInput{Key: runtime.SettingGroupRobotWebhookURL, Value: webhookValue}); err != nil {
			return err
		}
	}
	return nil
}

func (uc *AdminSettingsUseCase) suppressExpiredNotificationsBeforeEnable(ctx context.Context, inputs []UpdateSettingInput) error {
	now := time.Now()
	for _, input := range inputs {
		category, ok := groupRobotCategoryForSetting(input.Key)
		if !ok || !strings.EqualFold(strings.TrimSpace(input.Value), "true") {
			continue
		}
		existing, err := uc.settingRepo.GetByKey(ctx, input.Key)
		if err != nil {
			return err
		}
		if existing != nil && strings.EqualFold(strings.TrimSpace(existing.Value), "true") {
			continue
		}
		const reason = "notification category was disabled when due"
		if uc.notificationRepo != nil {
			if _, err := uc.notificationRepo.SuppressDueCategory(ctx, category, now, reason); err != nil {
				return err
			}
		}
		if category == types.GroupRobotCategoryFollowup && uc.reminderRepo != nil {
			if _, err := uc.reminderRepo.SuppressDue(ctx, types.ReminderTypeFollowupDue, now, reason); err != nil {
				return err
			}
		}
	}
	return nil
}

func groupRobotCategoryForSetting(key string) (string, bool) {
	switch key {
	case runtime.SettingGroupRobotEnabled:
		return types.GroupRobotCategoryNewLead, true
	case runtime.SettingDispatchClaimNotify:
		return types.GroupRobotCategoryDispatchClaim, true
	case runtime.SettingFollowupNotify:
		return types.GroupRobotCategoryFollowup, true
	case runtime.SettingPoolNotify:
		return types.GroupRobotCategoryPool, true
	default:
		return "", false
	}
}

func normalizeNonNegativeIntCSV(key, value string) (string, error) {
	parts := strings.Split(value, ",")
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return "", fmt.Errorf("%s must be a comma-separated list of non-negative integers", key)
		}
		normalized = append(normalized, strconv.Itoa(number))
	}
	if len(normalized) == 0 {
		return "", fmt.Errorf("%s must contain at least one value", key)
	}
	return strings.Join(normalized, ","), nil
}

func normalizePositiveIntCSV(key, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if isZeroCSV(value) {
		return "", nil
	}
	parts := strings.Split(value, ",")
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		number, err := strconv.Atoi(part)
		if err != nil || number <= 0 {
			return "", fmt.Errorf("%s must be a comma-separated list of positive integers", key)
		}
		normalized = append(normalized, strconv.Itoa(number))
	}
	if len(normalized) == 0 {
		return "", nil
	}
	return strings.Join(normalized, ","), nil
}

func isZeroCSV(value string) bool {
	parts := strings.Split(value, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" && part != "0" {
			return false
		}
	}
	return true
}

func shouldKeepExistingSensitiveValue(inputValue, existingValue string) bool {
	if existingValue == "" {
		return false
	}
	inputValue = strings.TrimSpace(inputValue)
	return inputValue == "" || inputValue == "******"
}

func maskSensitiveSettings(settings []*types.SystemSetting) []*types.SystemSetting {
	result := make([]*types.SystemSetting, len(settings))
	for i, setting := range settings {
		copySetting := *setting
		if copySetting.IsSensitive {
			if copySetting.Value != "" {
				copySetting.Value = "******"
			}
		}
		result[i] = &copySetting
	}
	return result
}
