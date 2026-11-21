package runtime

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/adapter/wecom"
	"github.com/lsaac128/kxone/pkg/types"
)

const (
	SettingWeComCorpID               = "wecom.corp_id"
	SettingWeComAgentID              = "wecom.agent_id"
	SettingWeComSecret               = "wecom.secret"
	SettingWeComToken                = "wecom.token"
	SettingWeComEncodingAESKey       = "wecom.encoding_aes_key"
	SettingDispatchReminderMins      = "reminder.dispatch_minutes"
	SettingClaimReminderMins         = "reminder.claim_minutes"
	SettingCardRetrySeconds          = "card.retry_seconds"
	SettingGroupRobotEnabled         = "notification.group_robot_enabled"
	SettingGroupRobotWebhookURL      = "notification.group_robot_webhook_url"
	SettingDispatchClaimNotify       = "notification.group_robot_dispatch_claim_enabled"
	SettingFollowupNotify            = "notification.group_robot_followup_enabled"
	SettingPoolNotify                = "notification.group_robot_pool_enabled"
	SettingGroupRobotMentionsEnabled = "notification.group_robot_mentions_enabled"
	SettingGroupRobotRulesRollout    = "notification.group_robot_rules_rollout_at"
	SettingWorkflowV2Enabled         = "workflow.v2_enabled"
	SettingAppointmentEnabled        = "appointment.enabled"
	SettingPoolEnabled               = "pool.enabled"
	SettingPoolAutoReturnEnabled     = "pool.auto_return_enabled"
	SettingFirstFollowupDueMins      = "followup.first_due_minutes"
	SettingFollowupReminderMins      = "followup.reminder_minutes"
	SettingStar1FollowupHours        = "followup.star_1_hours"
	SettingStar2FollowupHours        = "followup.star_2_hours"
	SettingStar3FollowupHours        = "followup.star_3_hours"
	SettingStar4FollowupHours        = "followup.star_4_hours"
	SettingStar5FollowupHours        = "followup.star_5_hours"
	SettingPoolMaxActiveLeads        = "pool.max_active_leads"
	SettingPoolCooldownMins          = "pool.cooldown_minutes"
	SettingPoolUnclaimedMins         = "pool.unclaimed_minutes"
	SettingPoolFollowupGraceMins     = "pool.followup_grace_minutes"
	SettingAppointmentDefaultMins    = "appointment.default_duration_minutes"
	SettingAppointmentBufferMins     = "appointment.buffer_minutes"
	SettingAppointmentDailyLimit     = "appointment.daily_soft_limit"
	SettingAppointmentWorkStart      = "appointment.workday_start_hour"
	SettingAppointmentWorkEnd        = "appointment.workday_end_hour"
	SettingAppointmentNoShowMins     = "appointment.no_show_followup_minutes"
)

// RuntimeSettings 提供运行期业务配置，优先读取后台配置，失败时回退默认值。
type RuntimeSettings interface {
	WeComAgentID(ctx context.Context) int
	DispatchReminderDelays(ctx context.Context) []time.Duration
	ClaimReminderDelays(ctx context.Context) []time.Duration
	CardRetryDelays(ctx context.Context) []time.Duration
}

// WorkflowFeatureFlags isolates the V2 rollout switches from the legacy
// RuntimeSettings contract. Existing use cases stay unchanged while the new
// workflow is developed behind defaults that are safe for production.
type WorkflowFeatureFlags interface {
	WorkflowV2Enabled(ctx context.Context) bool
	AppointmentEnabled(ctx context.Context) bool
	PoolEnabled(ctx context.Context) bool
	PoolAutoReturnEnabled(ctx context.Context) bool
}

type FollowupPolicySettings interface {
	FirstFollowupDue(ctx context.Context) time.Duration
	FollowupDueByStar(ctx context.Context, starRating int) time.Duration
	FollowupReminderOffsets(ctx context.Context) []time.Duration
	PoolMaxActiveLeads(ctx context.Context) int
	PoolCooldown(ctx context.Context) time.Duration
	PoolUnclaimedTimeout(ctx context.Context) time.Duration
	PoolFollowupGrace(ctx context.Context) time.Duration
}

type AppointmentPolicySettings interface {
	AppointmentDefaultDuration(ctx context.Context) time.Duration
	AppointmentBuffer(ctx context.Context) time.Duration
	AppointmentDailySoftLimit(ctx context.Context) int
	AppointmentWorkdayHours(ctx context.Context) (int, int)
	AppointmentNoShowFollowup(ctx context.Context) time.Duration
}

type GroupRobotNotificationSettings interface {
	GroupRobotCategoryEnabled(ctx context.Context, category string) bool
	GroupRobotMentionsEnabled(ctx context.Context) bool
	EnsureGroupRobotRulesRolloutCutoff(ctx context.Context, initializeAt time.Time) (time.Time, error)
}

type DatabaseRuntimeSettings struct {
	repo             repo.SystemSettingRepository
	defaultWeCom     wecom.Config
	defaultDispatch  []time.Duration
	defaultClaim     []time.Duration
	defaultCardRetry []time.Duration
	rolloutMu        sync.Mutex
}

type StaticRuntimeSettings struct {
	weCom     wecom.Config
	dispatch  []time.Duration
	claim     []time.Duration
	cardRetry []time.Duration
}

func NewDatabaseRuntimeSettings(settingRepo repo.SystemSettingRepository, defaultAgentID int) *DatabaseRuntimeSettings {
	return NewDatabaseRuntimeSettingsWithWeComConfig(settingRepo, wecom.Config{AgentID: defaultAgentID})
}

func NewDatabaseRuntimeSettingsWithWeComConfig(settingRepo repo.SystemSettingRepository, defaultWeCom wecom.Config) *DatabaseRuntimeSettings {
	if settingRepo == nil {
		panic("runtime: DatabaseRuntimeSettings requires a non-nil SystemSettingRepository")
	}
	defaults := NewStaticRuntimeSettings(defaultWeCom)
	return &DatabaseRuntimeSettings{
		repo:             settingRepo,
		defaultWeCom:     defaultWeCom,
		defaultDispatch:  defaults.DispatchReminderDelays(context.Background()),
		defaultClaim:     defaults.ClaimReminderDelays(context.Background()),
		defaultCardRetry: defaults.CardRetryDelays(context.Background()),
	}
}

func DefaultWeComConfig() wecom.Config {
	return wecom.Config{}
}

func NewStaticRuntimeSettings(defaultWeCom wecom.Config) *StaticRuntimeSettings {
	return &StaticRuntimeSettings{
		weCom:     defaultWeCom,
		dispatch:  []time.Duration{3 * time.Minute, 5 * time.Minute, 10 * time.Minute},
		claim:     []time.Duration{3 * time.Minute, 5 * time.Minute, 10 * time.Minute},
		cardRetry: []time.Duration{10 * time.Second, 30 * time.Second, 60 * time.Second},
	}
}

func (s *StaticRuntimeSettings) WeComAgentID(ctx context.Context) int {
	return s.weCom.AgentID
}

func (s *StaticRuntimeSettings) DispatchReminderDelays(ctx context.Context) []time.Duration {
	return cloneDurations(s.dispatch)
}

func (s *StaticRuntimeSettings) ClaimReminderDelays(ctx context.Context) []time.Duration {
	return cloneDurations(s.claim)
}

func (s *StaticRuntimeSettings) CardRetryDelays(ctx context.Context) []time.Duration {
	return cloneDurations(s.cardRetry)
}

func (s *StaticRuntimeSettings) WorkflowV2Enabled(context.Context) bool {
	return false
}

func (s *StaticRuntimeSettings) AppointmentEnabled(context.Context) bool {
	return false
}

func (s *StaticRuntimeSettings) PoolEnabled(context.Context) bool {
	return false
}

func (s *StaticRuntimeSettings) PoolAutoReturnEnabled(context.Context) bool {
	return false
}

func (s *StaticRuntimeSettings) GroupRobotCategoryEnabled(context.Context, string) bool {
	return false
}

func (s *StaticRuntimeSettings) GroupRobotMentionsEnabled(context.Context) bool { return false }

// Static settings have no durable storage, so they deliberately opt out of a
// rollout cutoff. Production workers use DatabaseRuntimeSettings below.
func (s *StaticRuntimeSettings) EnsureGroupRobotRulesRolloutCutoff(context.Context, time.Time) (time.Time, error) {
	return time.Time{}, nil
}

func (s *StaticRuntimeSettings) FirstFollowupDue(context.Context) time.Duration { return 2 * time.Hour }
func (s *StaticRuntimeSettings) FollowupDueByStar(_ context.Context, starRating int) time.Duration {
	return time.Duration(defaultStarFollowupHours(starRating)) * time.Hour
}
func (s *StaticRuntimeSettings) FollowupReminderOffsets(context.Context) []time.Duration {
	return []time.Duration{0, time.Hour, 4 * time.Hour}
}
func (s *StaticRuntimeSettings) PoolMaxActiveLeads(context.Context) int     { return 100 }
func (s *StaticRuntimeSettings) PoolCooldown(context.Context) time.Duration { return 24 * time.Hour }
func (s *StaticRuntimeSettings) PoolUnclaimedTimeout(context.Context) time.Duration {
	return 24 * time.Hour
}
func (s *StaticRuntimeSettings) PoolFollowupGrace(context.Context) time.Duration {
	return 4 * time.Hour
}
func (s *StaticRuntimeSettings) AppointmentDefaultDuration(context.Context) time.Duration {
	return 90 * time.Minute
}
func (s *StaticRuntimeSettings) AppointmentBuffer(context.Context) time.Duration {
	return 15 * time.Minute
}
func (s *StaticRuntimeSettings) AppointmentDailySoftLimit(context.Context) int { return 6 }
func (s *StaticRuntimeSettings) AppointmentWorkdayHours(context.Context) (int, int) {
	return 9, 18
}
func (s *StaticRuntimeSettings) AppointmentNoShowFollowup(context.Context) time.Duration {
	return time.Hour
}

func (s *DatabaseRuntimeSettings) WeComAgentID(ctx context.Context) int {
	return s.GetWeComConfig(ctx).AgentID
}

func (s *DatabaseRuntimeSettings) GetWeComConfig(ctx context.Context) wecom.Config {
	cfg := s.defaultWeCom
	cfg.CorpID = valueOrDefault(s.get(ctx, SettingWeComCorpID), cfg.CorpID)
	cfg.Secret = valueOrDefault(s.get(ctx, SettingWeComSecret), cfg.Secret)
	cfg.Token = valueOrDefault(s.get(ctx, SettingWeComToken), cfg.Token)
	cfg.EncodingAESKey = valueOrDefault(s.get(ctx, SettingWeComEncodingAESKey), cfg.EncodingAESKey)

	agentID := s.get(ctx, SettingWeComAgentID)
	if agentID != "" {
		parsed, err := strconv.Atoi(agentID)
		if err == nil {
			cfg.AgentID = parsed
		}
	}
	return cfg
}

func (s *DatabaseRuntimeSettings) DispatchReminderDelays(ctx context.Context) []time.Duration {
	return s.getMinuteDelays(ctx, SettingDispatchReminderMins, s.defaultDispatch)
}

func (s *DatabaseRuntimeSettings) ClaimReminderDelays(ctx context.Context) []time.Duration {
	return s.getMinuteDelays(ctx, SettingClaimReminderMins, s.defaultClaim)
}

func (s *DatabaseRuntimeSettings) CardRetryDelays(ctx context.Context) []time.Duration {
	return s.getSecondDelays(ctx, SettingCardRetrySeconds, s.defaultCardRetry)
}

func (s *DatabaseRuntimeSettings) WorkflowV2Enabled(ctx context.Context) bool {
	return s.getBool(ctx, SettingWorkflowV2Enabled, false)
}

func (s *DatabaseRuntimeSettings) AppointmentEnabled(ctx context.Context) bool {
	return s.getBool(ctx, SettingAppointmentEnabled, false)
}

func (s *DatabaseRuntimeSettings) PoolEnabled(ctx context.Context) bool {
	return s.getBool(ctx, SettingPoolEnabled, false)
}

func (s *DatabaseRuntimeSettings) PoolAutoReturnEnabled(ctx context.Context) bool {
	return s.PoolEnabled(ctx) && s.getBool(ctx, SettingPoolAutoReturnEnabled, false)
}

func (s *DatabaseRuntimeSettings) GroupRobotCategoryEnabled(ctx context.Context, category string) bool {
	key := ""
	switch category {
	case "new_lead":
		key = SettingGroupRobotEnabled
	case "dispatch_claim":
		key = SettingDispatchClaimNotify
	case "followup":
		key = SettingFollowupNotify
	case "pool":
		key = SettingPoolNotify
	}
	return key != "" && s.getBool(ctx, key, false)
}

func (s *DatabaseRuntimeSettings) GroupRobotMentionsEnabled(ctx context.Context) bool {
	return s.getBool(ctx, SettingGroupRobotMentionsEnabled, false)
}

type createSystemSettingIfAbsent interface {
	CreateIfAbsent(ctx context.Context, setting *types.SystemSetting) (bool, error)
}

// EnsureGroupRobotRulesRolloutCutoff returns one immutable, database-backed
// activation boundary for rule notifications. It is initialized on the first
// worker scan and never moves on restarts, so a restart cannot turn valid
// post-rollout work into suppressed history (or old work into a backfill).
func (s *DatabaseRuntimeSettings) EnsureGroupRobotRulesRolloutCutoff(ctx context.Context, initializeAt time.Time) (time.Time, error) {
	if s == nil || s.repo == nil {
		return time.Time{}, fmt.Errorf("group robot rules rollout settings are unavailable")
	}
	if initializeAt.IsZero() {
		initializeAt = time.Now()
	}
	initializeAt = initializeAt.UTC()

	s.rolloutMu.Lock()
	defer s.rolloutMu.Unlock()

	setting, err := s.repo.GetByKey(ctx, SettingGroupRobotRulesRollout)
	if err != nil {
		return time.Time{}, fmt.Errorf("read group robot rules rollout cutoff: %w", err)
	}
	if setting != nil {
		return parseGroupRobotRulesRolloutCutoff(setting.Value)
	}

	marker := &types.SystemSetting{
		Key: SettingGroupRobotRulesRollout, Value: initializeAt.Format(time.RFC3339Nano),
		ValueType: "datetime", Group: "internal", Description: "企业微信群规则通知首次上线分界",
		UpdatedBy: "system:group-robot-worker", CreatedAt: initializeAt, UpdatedAt: initializeAt,
	}
	if initializer, ok := s.repo.(createSystemSettingIfAbsent); ok {
		if _, err := initializer.CreateIfAbsent(ctx, marker); err != nil {
			return time.Time{}, fmt.Errorf("initialize group robot rules rollout cutoff: %w", err)
		}
	} else if err := s.repo.Upsert(ctx, marker); err != nil {
		return time.Time{}, fmt.Errorf("initialize group robot rules rollout cutoff: %w", err)
	}

	// Always read back: another worker may have won the unique-key race with an
	// earlier timestamp, and that first persisted value is the authoritative one.
	setting, err = s.repo.GetByKey(ctx, SettingGroupRobotRulesRollout)
	if err != nil {
		return time.Time{}, fmt.Errorf("read initialized group robot rules rollout cutoff: %w", err)
	}
	if setting == nil {
		return time.Time{}, fmt.Errorf("group robot rules rollout cutoff was not persisted")
	}
	return parseGroupRobotRulesRolloutCutoff(setting.Value)
}

func parseGroupRobotRulesRolloutCutoff(raw string) (time.Time, error) {
	cutoff, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(raw))
	if err != nil || cutoff.IsZero() {
		return time.Time{}, fmt.Errorf("invalid %s value %q", SettingGroupRobotRulesRollout, raw)
	}
	return cutoff.UTC(), nil
}

func (s *DatabaseRuntimeSettings) FirstFollowupDue(ctx context.Context) time.Duration {
	return time.Duration(s.getPositiveInt(ctx, SettingFirstFollowupDueMins, 120)) * time.Minute
}

func (s *DatabaseRuntimeSettings) FollowupDueByStar(ctx context.Context, starRating int) time.Duration {
	keys := map[int]string{1: SettingStar1FollowupHours, 2: SettingStar2FollowupHours, 3: SettingStar3FollowupHours, 4: SettingStar4FollowupHours, 5: SettingStar5FollowupHours}
	return time.Duration(s.getPositiveInt(ctx, keys[starRating], defaultStarFollowupHours(starRating))) * time.Hour
}

func (s *DatabaseRuntimeSettings) FollowupReminderOffsets(ctx context.Context) []time.Duration {
	raw, exists := s.getSetting(ctx, SettingFollowupReminderMins)
	if !exists || raw == "" {
		return []time.Duration{0, time.Hour, 4 * time.Hour}
	}
	parts := strings.Split(raw, ",")
	result := make([]time.Duration, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err == nil && value >= 0 {
			result = append(result, time.Duration(value)*time.Minute)
		}
	}
	if len(result) == 0 {
		return []time.Duration{0, time.Hour, 4 * time.Hour}
	}
	return result
}

func (s *DatabaseRuntimeSettings) PoolMaxActiveLeads(ctx context.Context) int {
	return s.getPositiveInt(ctx, SettingPoolMaxActiveLeads, 100)
}
func (s *DatabaseRuntimeSettings) PoolCooldown(ctx context.Context) time.Duration {
	return time.Duration(s.getPositiveInt(ctx, SettingPoolCooldownMins, 1440)) * time.Minute
}
func (s *DatabaseRuntimeSettings) PoolUnclaimedTimeout(ctx context.Context) time.Duration {
	return time.Duration(s.getPositiveInt(ctx, SettingPoolUnclaimedMins, 1440)) * time.Minute
}
func (s *DatabaseRuntimeSettings) PoolFollowupGrace(ctx context.Context) time.Duration {
	return time.Duration(s.getPositiveInt(ctx, SettingPoolFollowupGraceMins, 240)) * time.Minute
}
func (s *DatabaseRuntimeSettings) AppointmentDefaultDuration(ctx context.Context) time.Duration {
	return time.Duration(s.getPositiveInt(ctx, SettingAppointmentDefaultMins, 90)) * time.Minute
}
func (s *DatabaseRuntimeSettings) AppointmentBuffer(ctx context.Context) time.Duration {
	return time.Duration(s.getPositiveInt(ctx, SettingAppointmentBufferMins, 15)) * time.Minute
}
func (s *DatabaseRuntimeSettings) AppointmentDailySoftLimit(ctx context.Context) int {
	return s.getPositiveInt(ctx, SettingAppointmentDailyLimit, 6)
}
func (s *DatabaseRuntimeSettings) AppointmentWorkdayHours(ctx context.Context) (int, int) {
	start := s.getPositiveInt(ctx, SettingAppointmentWorkStart, 9)
	end := s.getPositiveInt(ctx, SettingAppointmentWorkEnd, 18)
	if start >= end || end > 24 {
		return 9, 18
	}
	return start, end
}
func (s *DatabaseRuntimeSettings) AppointmentNoShowFollowup(ctx context.Context) time.Duration {
	return time.Duration(s.getPositiveInt(ctx, SettingAppointmentNoShowMins, 60)) * time.Minute
}

func (s *DatabaseRuntimeSettings) getPositiveInt(ctx context.Context, key string, fallback int) int {
	value, err := strconv.Atoi(s.get(ctx, key))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func defaultStarFollowupHours(starRating int) int {
	switch starRating {
	case 1:
		return 168
	case 2:
		return 120
	case 3:
		return 72
	case 4:
		return 48
	case 5:
		return 24
	default:
		return 72
	}
}

func (s *DatabaseRuntimeSettings) get(ctx context.Context, key string) string {
	value, _ := s.getSetting(ctx, key)
	return value
}

func (s *DatabaseRuntimeSettings) getSetting(ctx context.Context, key string) (string, bool) {
	if s == nil {
		return "", false
	}
	setting, err := s.repo.GetByKey(ctx, key)
	if err != nil || setting == nil {
		return "", false
	}
	return strings.TrimSpace(setting.Value), true
}

func (s *DatabaseRuntimeSettings) getBool(ctx context.Context, key string, fallback bool) bool {
	raw, exists := s.getSetting(ctx, key)
	if !exists {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return value
}

func (s *DatabaseRuntimeSettings) getMinuteDelays(ctx context.Context, key string, fallback []time.Duration) []time.Duration {
	return s.getDurationDelays(ctx, key, time.Minute, fallback)
}

func (s *DatabaseRuntimeSettings) getSecondDelays(ctx context.Context, key string, fallback []time.Duration) []time.Duration {
	return s.getDurationDelays(ctx, key, time.Second, fallback)
}

func (s *DatabaseRuntimeSettings) getDurationDelays(ctx context.Context, key string, unit time.Duration, fallback []time.Duration) []time.Duration {
	raw, exists := s.getSetting(ctx, key)
	if !exists {
		return cloneDurations(fallback)
	}
	if raw == "" || isDisabledDurationList(raw) {
		return []time.Duration{}
	}
	delays := parseDurationList(raw, unit)
	if len(delays) == 0 {
		return cloneDurations(fallback)
	}
	return delays
}

func isDisabledDurationList(raw string) bool {
	parts := strings.Split(raw, ",")
	if len(parts) == 0 {
		return false
	}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" && part != "0" {
			return false
		}
	}
	return true
}

func parseDurationList(raw string, unit time.Duration) []time.Duration {
	values := strings.Split(raw, ",")
	var delays []time.Duration
	for _, item := range values {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		value, err := strconv.Atoi(item)
		if err != nil || value <= 0 {
			continue
		}
		delays = append(delays, time.Duration(value)*unit)
	}
	return delays
}

func cloneDurations(values []time.Duration) []time.Duration {
	result := make([]time.Duration, len(values))
	copy(result, values)
	return result
}

func valueOrDefault(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
