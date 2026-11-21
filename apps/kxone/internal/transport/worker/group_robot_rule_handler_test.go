package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/adapter/wecom"
	"github.com/lsaac128/kxone/internal/config"
	"github.com/lsaac128/kxone/internal/runtime"
	"github.com/lsaac128/kxone/pkg/types"
	"gorm.io/gorm"
)

type groupRobotHandlerSettings struct {
	*runtime.StaticRuntimeSettings
	categories map[string]bool
	dispatch   []time.Duration
	claim      []time.Duration
	followup   []time.Duration
}

func newGroupRobotHandlerSettings() *groupRobotHandlerSettings {
	return &groupRobotHandlerSettings{
		StaticRuntimeSettings: runtime.NewStaticRuntimeSettings(wecom.Config{}),
		categories:            map[string]bool{},
		dispatch:              []time.Duration{3 * time.Minute},
		claim:                 []time.Duration{5 * time.Minute},
		followup:              []time.Duration{0, time.Hour, 4 * time.Hour},
	}
}

func (s *groupRobotHandlerSettings) DispatchReminderDelays(context.Context) []time.Duration {
	return append([]time.Duration(nil), s.dispatch...)
}

func (s *groupRobotHandlerSettings) ClaimReminderDelays(context.Context) []time.Duration {
	return append([]time.Duration(nil), s.claim...)
}

func (s *groupRobotHandlerSettings) FollowupReminderOffsets(context.Context) []time.Duration {
	return append([]time.Duration(nil), s.followup...)
}

func (s *groupRobotHandlerSettings) GroupRobotCategoryEnabled(_ context.Context, category string) bool {
	return s.categories[category]
}

type recordingGroupRobotSender struct {
	contents []string
	err      error
}

func (s *recordingGroupRobotSender) SendMarkdown(_ context.Context, content string) error {
	s.contents = append(s.contents, content)
	return s.err
}

type failingInitialGroupRobotWriter struct {
	repo.GroupRobotNotificationRepository
}

func (w *failingInitialGroupRobotWriter) Create(context.Context, *types.GroupRobotNotification) (bool, error) {
	return false, errors.New("initial outbox write failed")
}

func TestGroupRobotRuleHandlerRendersCompleteMaskedMessages(t *testing.T) {
	dueAt := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	now := dueAt.Add(20 * time.Minute)
	baseLead := &types.Lead{
		ID: 9, LeadNo: "LEAD-9", CustomerName: "张同学",
		CustomerMobile: "13912345678", CustomerWechat: "wxsecret01",
		CustomerMobileMasked: "13912345678", CustomerWechatMasked: "wxsecret01",
		SourceChannel: "ad:tencent:武汉", ProductName: "夏令营", StarRating: 4,
		DispatchManagerUserID: "manager-1", AssignedInviterUserID: "inviter-1",
	}
	tests := []struct {
		name      string
		eventType string
		status    types.LeadStatus
		payload   groupRobotEventPayload
		wantOwner string
		wantRule  string
	}{
		{name: "new lead", eventType: types.GroupRobotEventNewLead, status: types.LeadStatusPendingDispatch, wantOwner: "manager-1", wantRule: "新线索即时通知"},
		{name: "dispatch", eventType: types.GroupRobotEventDispatchOverdue, status: types.LeadStatusPendingDispatch, payload: groupRobotEventPayload{ReminderMinutes: 3}, wantOwner: "manager-1", wantRule: "创建后 3 分钟仍未分配"},
		{name: "claim", eventType: types.GroupRobotEventClaimOverdue, status: types.LeadStatusAssignedPendingClaim, payload: groupRobotEventPayload{ReminderMinutes: 5}, wantOwner: "inviter-1", wantRule: "分配后 5 分钟仍未领取"},
		{name: "followup", eventType: types.GroupRobotEventFollowupDue, status: types.LeadStatusFollowing, payload: groupRobotEventPayload{ReceiverUserIDs: []string{"inviter-1"}}, wantOwner: "inviter-1", wantRule: "4 星线索回访到期"},
		{name: "pool", eventType: types.GroupRobotEventPoolAutoReturn, status: types.LeadStatusPublicPool, payload: groupRobotEventPayload{PreviousOwnerID: "inviter-1", Reason: types.PoolReasonFollowupOverdue}, wantOwner: "inviter-1", wantRule: "自动回公海"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lead := *baseLead
			lead.Status = test.status
			payload, _ := json.Marshal(test.payload)
			handler := NewGroupRobotRuleHandler(nil, nil, nil, nil, newGroupRobotHandlerSettings(), &recordingGroupRobotSender{}, nil, "https://crm.example.com")
			content, err := handler.renderNotification(context.Background(), &types.GroupRobotNotification{
				EventType: test.eventType, DueAt: dueAt, PayloadJSON: string(payload),
			}, &lead, now)
			if err != nil {
				t.Fatal(err)
			}
			for _, expected := range []string{
				"线索编号：LEAD-9", "手机：139****5678", "微信：wx****01",
				"来源：ad:tencent:武汉", "状态：", "负责人：@" + test.wantOwner,
				"规则：" + test.wantRule, "到期时间：2026-08-06 18:00",
				"https://crm.example.com/leads/9",
			} {
				if !strings.Contains(content, expected) {
					t.Fatalf("message missing %q:\n%s", expected, content)
				}
			}
			if strings.Contains(content, "13912345678") || strings.Contains(content, "wxsecret01") || strings.Contains(content, "<@") {
				t.Fatalf("message leaked contact data or mentioned a user:\n%s", content)
			}
		})
	}
}

func TestGroupRobotRuleHandlerCategorySwitchesAreIsolatedAndSuppressedItemsDoNotBackfill(t *testing.T) {
	categories := []string{
		types.GroupRobotCategoryNewLead,
		types.GroupRobotCategoryDispatchClaim,
		types.GroupRobotCategoryFollowup,
		types.GroupRobotCategoryPool,
	}
	for _, enabledCategory := range categories {
		t.Run(enabledCategory, func(t *testing.T) {
			db, leads, notifications := newGroupRobotHandlerTestRepositories(t)
			_ = db
			now := time.Now().Add(-time.Minute)
			lead := groupRobotTestLead(now)
			if err := leads.Create(context.Background(), lead); err != nil {
				t.Fatal(err)
			}
			for index, category := range categories {
				_, err := notifications.Create(context.Background(), &types.GroupRobotNotification{
					EventKey: "switch-" + category, Category: category, EventType: types.GroupRobotEventNewLead,
					LeadID: lead.ID, BusinessVersion: lead.Version, DueAt: now.Add(time.Duration(index) * time.Millisecond),
					Status: types.GroupRobotNotificationPending, CreatedAt: now, UpdatedAt: now,
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			settings := newGroupRobotHandlerSettings()
			settings.categories[enabledCategory] = true
			sender := &recordingGroupRobotSender{}
			handler := NewGroupRobotRuleHandler(leads, nil, notifications, nil, settings, sender, nil, "https://crm.example.com")
			if err := handler.deliverReady(context.Background(), time.Now()); err != nil {
				t.Fatal(err)
			}
			if len(sender.contents) != 1 {
				t.Fatalf("sent messages = %d, want 1", len(sender.contents))
			}
			for _, category := range categories {
				record, err := notifications.GetByEventKey(context.Background(), "switch-"+category)
				if err != nil {
					t.Fatal(err)
				}
				want := types.GroupRobotNotificationSuppressed
				if category == enabledCategory {
					want = types.GroupRobotNotificationSent
				}
				if record.Status != want {
					t.Fatalf("%s status = %s, want %s", category, record.Status, want)
				}
			}
			for _, category := range categories {
				settings.categories[category] = true
			}
			if err := handler.deliverReady(context.Background(), time.Now().Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			if len(sender.contents) != 1 {
				t.Fatalf("suppressed history was backfilled; sent=%d", len(sender.contents))
			}
		})
	}
}

func TestGroupRobotRuleHandlerCancelsChangedStateAndRule(t *testing.T) {
	_, leads, notifications := newGroupRobotHandlerTestRepositories(t)
	now := time.Now().Add(-time.Hour)
	lead := groupRobotTestLead(now)
	if err := leads.Create(context.Background(), lead); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(groupRobotEventPayload{ReminderMinutes: 99})
	_, err := notifications.Create(context.Background(), &types.GroupRobotNotification{
		EventKey: "changed-rule", Category: types.GroupRobotCategoryDispatchClaim,
		EventType: types.GroupRobotEventDispatchOverdue, LeadID: lead.ID, BusinessVersion: lead.Version,
		DueAt: now, Status: types.GroupRobotNotificationPending, PayloadJSON: string(payload), CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimPayload, _ := json.Marshal(groupRobotEventPayload{ReminderMinutes: 5})
	_, err = notifications.Create(context.Background(), &types.GroupRobotNotification{
		EventKey: "changed-state", Category: types.GroupRobotCategoryDispatchClaim,
		EventType: types.GroupRobotEventClaimOverdue, LeadID: lead.ID, BusinessVersion: lead.Version,
		DueAt: now, Status: types.GroupRobotNotificationPending, PayloadJSON: string(claimPayload), CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	settings := newGroupRobotHandlerSettings()
	settings.categories[types.GroupRobotCategoryDispatchClaim] = true
	sender := &recordingGroupRobotSender{}
	handler := NewGroupRobotRuleHandler(leads, nil, notifications, nil, settings, sender, nil, "https://crm.example.com")
	if err := handler.deliverReady(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, eventKey := range []string{"changed-rule", "changed-state"} {
		record, err := notifications.GetByEventKey(context.Background(), eventKey)
		if err != nil {
			t.Fatal(err)
		}
		if record.Status != types.GroupRobotNotificationCancelled {
			t.Fatalf("%s status=%s", eventKey, record.Status)
		}
	}
	if len(sender.contents) != 0 {
		t.Fatalf("cancelled notifications sent=%d", len(sender.contents))
	}
}

func TestGroupRobotRuleHandlerStillSendsNewLeadAfterFastAssignment(t *testing.T) {
	_, leads, notifications := newGroupRobotHandlerTestRepositories(t)
	now := time.Now().Add(-time.Minute)
	lead := groupRobotTestLead(now)
	lead.Status = types.LeadStatusAssignedPendingClaim
	lead.Version = 2
	lead.AssignedInviterUserID = "inviter-1"
	if err := leads.Create(context.Background(), lead); err != nil {
		t.Fatal(err)
	}
	_, err := notifications.Create(context.Background(), &types.GroupRobotNotification{
		EventKey: "new-before-assignment", Category: types.GroupRobotCategoryNewLead,
		EventType: types.GroupRobotEventNewLead, LeadID: lead.ID, BusinessVersion: 1,
		DueAt: now, Status: types.GroupRobotNotificationPending, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	settings := newGroupRobotHandlerSettings()
	settings.categories[types.GroupRobotCategoryNewLead] = true
	sender := &recordingGroupRobotSender{}
	handler := NewGroupRobotRuleHandler(leads, nil, notifications, nil, settings, sender, nil, "https://crm.example.com")
	if err := handler.deliverReady(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	record, err := notifications.GetByEventKey(context.Background(), "new-before-assignment")
	if err != nil || record.Status != types.GroupRobotNotificationSent || len(sender.contents) != 1 {
		t.Fatalf("record=%+v sends=%d err=%v", record, len(sender.contents), err)
	}
}

func TestGroupRobotRuleHandlerReconcilesMissingNewLeadAfterInitialOutboxFailure(t *testing.T) {
	_, leads, notifications := newGroupRobotHandlerTestRepositories(t)
	ctx := context.Background()
	cutoff := time.Now().Add(-time.Minute).Truncate(time.Second)
	historical := groupRobotTestLead(cutoff.Add(-time.Second))
	historical.LeadNo = "PRE-ROLLOUT-NEW-LEAD"
	historical.Status = types.LeadStatusConverted
	if err := leads.Create(ctx, historical); err != nil {
		t.Fatal(err)
	}
	lead := groupRobotTestLead(cutoff.Add(time.Second))
	lead.LeadNo = "MISSING-NEW-LEAD-OUTBOX"
	lead.Status = types.LeadStatusConverted
	if err := leads.Create(ctx, lead); err != nil {
		t.Fatal(err)
	}

	failingWriter := &failingInitialGroupRobotWriter{GroupRobotNotificationRepository: notifications}
	notifier := wecom.NewDatabaseGroupRobotNotifier(nil).WithNotificationRepository(failingWriter)
	if err := notifier.NotifyLead(ctx, wecom.GroupRobotLead{LeadID: lead.ID, BusinessVersion: lead.Version}); err == nil {
		t.Fatal("initial notification write unexpectedly succeeded")
	}
	if record, err := notifications.GetByEventKey(ctx, "new-lead:"+strconv.FormatInt(lead.ID, 10)); err != nil || record != nil {
		t.Fatalf("initial failure persisted record=%+v err=%v", record, err)
	}

	settings := newGroupRobotHandlerSettings()
	settings.categories[types.GroupRobotCategoryNewLead] = true
	sender := &recordingGroupRobotSender{}
	handler := NewGroupRobotRuleHandler(leads, nil, notifications, nil, settings, sender, nil, "https://crm.example.com")
	handler.rolloutCutoff = cutoff
	handler.now = func() time.Time { return cutoff.Add(2 * time.Minute) }
	if err := handler.HandleScan(ctx, nil); err != nil {
		t.Fatal(err)
	}
	eventKey := "new-lead:" + strconv.FormatInt(lead.ID, 10)
	record, err := notifications.GetByEventKey(ctx, eventKey)
	if err != nil || record == nil || record.Status != types.GroupRobotNotificationSent || len(sender.contents) != 1 {
		t.Fatalf("reconciled record=%+v sends=%d err=%v", record, len(sender.contents), err)
	}
	if historicalRecord, err := notifications.GetByEventKey(ctx, "new-lead:"+strconv.FormatInt(historical.ID, 10)); err != nil || historicalRecord != nil {
		t.Fatalf("pre-rollout lead was compensated: record=%+v err=%v", historicalRecord, err)
	}
	if err := handler.HandleScan(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if len(sender.contents) != 1 {
		t.Fatalf("reconciliation duplicated new-lead delivery: sends=%d", len(sender.contents))
	}
}

func TestGroupRobotRuleHandlerReconcilesOnlyAutomaticPostCutoffPoolEvents(t *testing.T) {
	db, leads, notifications := newGroupRobotHandlerTestRepositories(t)
	ctx := context.Background()
	cutoff := time.Now().Add(-time.Minute).Truncate(time.Second)
	poolEnteredAt := cutoff.Add(4 * time.Second)
	lead := groupRobotTestLead(cutoff.Add(-time.Hour))
	lead.LeadNo = "MISSING-POOL-OUTBOX"
	lead.Status = types.LeadStatusPublicPool
	lead.Version = 7
	lead.OwnershipState = types.OwnershipPublicPool
	lead.PoolEnteredAt = &poolEnteredAt
	if err := leads.Create(ctx, lead); err != nil {
		t.Fatal(err)
	}
	poolRecords := repo.NewGormLeadPoolRepository(db)
	for _, record := range []*types.LeadPoolRecord{
		{LeadID: lead.ID, Action: types.PoolActionEnter, Reason: types.PoolReasonUnclaimedTimeout, PreviousOwnerUserID: "old-owner", ActorUserID: "system", PoolVersion: 3, CreatedAt: cutoff.Add(-time.Second)},
		{LeadID: lead.ID, Action: types.PoolActionEnter, Reason: types.PoolReasonManual, PreviousOwnerUserID: "manual-owner", ActorUserID: "manager", PoolVersion: 4, CreatedAt: cutoff.Add(time.Second)},
		{LeadID: lead.ID, Action: types.PoolActionClaim, Reason: types.PoolReasonFollowupOverdue, PreviousOwnerUserID: "claim-owner", ActorUserID: "inviter", PoolVersion: 5, CreatedAt: cutoff.Add(2 * time.Second)},
		{LeadID: lead.ID, Action: types.PoolActionEnter, Reason: types.PoolReasonFollowupOverdue, PreviousOwnerUserID: "manual-auto-reason-owner", ActorUserID: "manager", PoolVersion: 6, CreatedAt: cutoff.Add(3 * time.Second)},
		{LeadID: lead.ID, Action: types.PoolActionEnter, Reason: types.PoolReasonFollowupOverdue, PreviousOwnerUserID: "automatic-owner", ActorUserID: "system", PoolVersion: lead.Version, CreatedAt: poolEnteredAt},
	} {
		if err := poolRecords.Create(ctx, record); err != nil {
			t.Fatal(err)
		}
	}

	settings := newGroupRobotHandlerSettings()
	settings.categories[types.GroupRobotCategoryPool] = true
	sender := &recordingGroupRobotSender{}
	handler := NewGroupRobotRuleHandler(leads, nil, notifications, nil, settings, sender, nil, "https://crm.example.com")
	handler.rolloutCutoff = cutoff
	handler.now = func() time.Time { return cutoff.Add(2 * time.Minute) }
	if err := handler.HandleScan(ctx, nil); err != nil {
		t.Fatal(err)
	}
	eventKey := fmt.Sprintf("pool-auto-return:%d:v%d", lead.ID, lead.Version)
	record, err := notifications.GetByEventKey(ctx, eventKey)
	if err != nil || record == nil || record.Status != types.GroupRobotNotificationSent || len(sender.contents) != 1 {
		t.Fatalf("reconciled pool record=%+v sends=%d err=%v", record, len(sender.contents), err)
	}
	payload := decodeGroupRobotPayload(record.PayloadJSON)
	if payload.Reason != types.PoolReasonFollowupOverdue || payload.PreviousOwnerID != "automatic-owner" {
		t.Fatalf("reconciled pool payload=%+v", payload)
	}
	for _, excludedVersion := range []int{3, 4, 5, 6} {
		excluded, err := notifications.GetByEventKey(ctx, fmt.Sprintf("pool-auto-return:%d:v%d", lead.ID, excludedVersion))
		if err != nil || excluded != nil {
			t.Fatalf("excluded pool event v%d was compensated: record=%+v err=%v", excludedVersion, excluded, err)
		}
	}
	if err := handler.HandleScan(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if len(sender.contents) != 1 {
		t.Fatalf("reconciliation duplicated pool delivery: sends=%d", len(sender.contents))
	}
}

func TestGroupRobotRuleHandlerCancelsFollowupWhenOffsetRuleChanges(t *testing.T) {
	_, leads, notifications := newGroupRobotHandlerTestRepositories(t)
	now := time.Now().Add(-time.Hour).Truncate(time.Second)
	nextDue := now.Add(-time.Hour)
	lead := groupRobotTestLead(now.Add(-24 * time.Hour))
	lead.Status = types.LeadStatusFollowing
	lead.Version = 3
	lead.BusinessStage = types.BusinessStageInvitation
	lead.OwnershipState = types.OwnershipPrivate
	lead.ResultStatus = types.LeadResultActive
	lead.CurrentOwnerUserID = "inviter-1"
	lead.NextFollowupDueAt = &nextDue
	if err := leads.Create(context.Background(), lead); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(groupRobotEventPayload{FollowupOffsetMinutes: 60, ReceiverUserIDs: []string{"inviter-1"}})
	_, err := notifications.Create(context.Background(), &types.GroupRobotNotification{
		EventKey: "changed-followup-offset", Category: types.GroupRobotCategoryFollowup,
		EventType: types.GroupRobotEventFollowupDue, LeadID: lead.ID, BusinessVersion: lead.Version,
		DueAt: nextDue.Add(time.Hour), Status: types.GroupRobotNotificationPending, PayloadJSON: string(payload),
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	settings := newGroupRobotHandlerSettings()
	settings.followup = []time.Duration{0}
	settings.categories[types.GroupRobotCategoryFollowup] = true
	sender := &recordingGroupRobotSender{}
	handler := NewGroupRobotRuleHandler(leads, nil, notifications, nil, settings, sender, nil, "https://crm.example.com")
	if err := handler.deliverReady(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	record, err := notifications.GetByEventKey(context.Background(), "changed-followup-offset")
	if err != nil || record.Status != types.GroupRobotNotificationCancelled || len(sender.contents) != 0 {
		t.Fatalf("record=%+v sends=%d err=%v", record, len(sender.contents), err)
	}
}

func TestGroupRobotRuleHandlerSuppressesPreRolloutBusinessEventEvenWhenDueLater(t *testing.T) {
	_, leads, notifications := newGroupRobotHandlerTestRepositories(t)
	activation := time.Now().Truncate(time.Second)
	historical := groupRobotTestLead(activation.Add(-2 * time.Minute))
	historical.LeadNo = "HISTORICAL"
	if err := leads.Create(context.Background(), historical); err != nil {
		t.Fatal(err)
	}
	future := groupRobotTestLead(activation)
	future.LeadNo = "FUTURE"
	if err := leads.Create(context.Background(), future); err != nil {
		t.Fatal(err)
	}
	assignedAt := activation.Add(-2 * time.Minute)
	historicalClaim := groupRobotTestLead(activation.Add(-time.Hour))
	historicalClaim.LeadNo = "HISTORICAL-CLAIM"
	historicalClaim.Status = types.LeadStatusAssignedPendingClaim
	historicalClaim.AssignedAt = &assignedAt
	if err := leads.Create(context.Background(), historicalClaim); err != nil {
		t.Fatal(err)
	}
	settings := newGroupRobotHandlerSettings()
	handler := NewGroupRobotRuleHandler(leads, nil, notifications, nil, settings, &recordingGroupRobotSender{}, nil, "https://crm.example.com")
	handler.rolloutCutoff = activation
	if err := handler.reconcileDispatchAndClaim(context.Background(), activation); err != nil {
		t.Fatal(err)
	}
	historicalRecord, err := notifications.GetByEventKey(context.Background(), "dispatch:"+strconv.FormatInt(historical.ID, 10)+":v1:m3")
	if err != nil || historicalRecord.Status != types.GroupRobotNotificationSuppressed {
		t.Fatalf("historical=%+v err=%v", historicalRecord, err)
	}
	if !historicalRecord.DueAt.After(activation) {
		t.Fatalf("test boundary invalid: historical reminder due_at=%v cutoff=%v", historicalRecord.DueAt, activation)
	}
	futureRecord, err := notifications.GetByEventKey(context.Background(), "dispatch:"+strconv.FormatInt(future.ID, 10)+":v1:m3")
	if err != nil || futureRecord.Status != types.GroupRobotNotificationPending {
		t.Fatalf("future=%+v err=%v", futureRecord, err)
	}
	historicalClaimRecord, err := notifications.GetByEventKey(context.Background(), "claim:"+strconv.FormatInt(historicalClaim.ID, 10)+":v1:m5")
	if err != nil || historicalClaimRecord.Status != types.GroupRobotNotificationSuppressed || !historicalClaimRecord.DueAt.After(activation) {
		t.Fatalf("historical claim=%+v err=%v", historicalClaimRecord, err)
	}
}

func TestGroupRobotRuleHandlerPersistsRolloutCutoffAcrossRestartAndEnable(t *testing.T) {
	db, leads, notifications := newGroupRobotHandlerTestRepositories(t)
	if err := db.AutoMigrate(&repo.SystemSettingModel{}, &repo.LeadReminderRecordModel{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	activation := time.Date(2026, 8, 6, 8, 0, 0, 0, time.UTC)
	firstScanAt := activation.Add(time.Minute)
	restartAt := activation.Add(time.Hour)
	lead := groupRobotTestLead(activation.Add(-2 * time.Minute))
	if err := leads.Create(ctx, lead); err != nil {
		t.Fatal(err)
	}
	// This lead arrives after Worker construction but before its first scan. It
	// must be treated as post-rollout work even though the marker is persisted
	// only when that first scan runs.
	postLead := groupRobotTestLead(activation.Add(30 * time.Second))
	postLead.LeadNo = "POST-ROLLOUT"
	if err := leads.Create(ctx, postLead); err != nil {
		t.Fatal(err)
	}

	settingRepo := repo.NewGormSystemSettingRepository(db)
	upsertRuntimeSetting := func(key, value string) {
		t.Helper()
		if err := settingRepo.Upsert(ctx, &types.SystemSetting{
			Key: key, Value: value, ValueType: "boolean", Group: "notification",
			CreatedAt: activation, UpdatedAt: activation,
		}); err != nil {
			t.Fatal(err)
		}
	}
	upsertRuntimeSetting(runtime.SettingGroupRobotEnabled, "true")
	upsertRuntimeSetting(runtime.SettingDispatchClaimNotify, "false")
	upsertRuntimeSetting(runtime.SettingDispatchReminderMins, "3")

	legacyCreatedAt := activation.Add(-2 * time.Minute)
	legacyDue := activation.Add(time.Minute)
	newLeadDue := activation.Add(-time.Minute)
	futureRetry := restartAt.Add(time.Hour)
	for _, notification := range []*types.GroupRobotNotification{
		{
			EventKey: "legacy-pending", Category: types.GroupRobotCategoryDispatchClaim,
			EventType: types.GroupRobotEventDispatchOverdue, LeadID: lead.ID, BusinessVersion: lead.Version,
			DueAt: legacyDue, Status: types.GroupRobotNotificationPending,
			CreatedAt: legacyCreatedAt, UpdatedAt: legacyCreatedAt,
		},
		{
			EventKey: "legacy-failed", Category: types.GroupRobotCategoryDispatchClaim,
			EventType: types.GroupRobotEventDispatchOverdue, LeadID: lead.ID, BusinessVersion: lead.Version,
			DueAt: legacyDue, Status: types.GroupRobotNotificationFailed, NextRetryAt: &futureRetry,
			CreatedAt: legacyCreatedAt, UpdatedAt: legacyCreatedAt,
		},
		{
			EventKey: "legacy-due-new-lead", Category: types.GroupRobotCategoryNewLead,
			EventType: types.GroupRobotEventNewLead, LeadID: lead.ID, BusinessVersion: lead.Version,
			DueAt: newLeadDue, Status: types.GroupRobotNotificationPending,
			CreatedAt: legacyCreatedAt, UpdatedAt: legacyCreatedAt,
		},
	} {
		if _, err := notifications.Create(ctx, notification); err != nil {
			t.Fatal(err)
		}
	}
	reminders := repo.NewGormLeadReminderRepository(db)
	legacyReminder := &types.LeadReminderRecord{
		LeadID: lead.ID, ReminderType: types.ReminderTypeFollowupDue, BusinessVersion: lead.Version,
		DueAt: activation.Add(time.Hour), ReceiverUserID: "inviter-1", Status: types.ReminderStatusPending,
		IdempotencyKey: "legacy-followup-before-rollout", CreatedAt: legacyCreatedAt, UpdatedAt: legacyCreatedAt,
	}
	if _, err := reminders.Create(ctx, legacyReminder); err != nil {
		t.Fatal(err)
	}

	firstSender := &recordingGroupRobotSender{}
	firstHandler := NewGroupRobotRuleHandler(
		leads, reminders, notifications, nil,
		runtime.NewDatabaseRuntimeSettings(settingRepo, 0), firstSender, nil, "https://crm.example.com",
	)
	firstHandler.rolloutInitAt = activation
	firstHandler.now = func() time.Time { return firstScanAt }
	if err := firstHandler.HandleScan(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if len(firstSender.contents) != 1 || !strings.Contains(firstSender.contents[0], "新线索进入 CRM") {
		t.Fatalf("pre-rollout new-lead outbox was not delivered: %#v", firstSender.contents)
	}
	for _, key := range []string{"legacy-pending", "legacy-failed"} {
		record, err := notifications.GetByEventKey(ctx, key)
		if err != nil || record.Status != types.GroupRobotNotificationSuppressed {
			t.Fatalf("%s record=%+v err=%v", key, record, err)
		}
	}
	generatedHistorical, err := notifications.GetByEventKey(ctx, "dispatch:"+strconv.FormatInt(lead.ID, 10)+":v1:m3")
	if err != nil || generatedHistorical == nil || generatedHistorical.Status != types.GroupRobotNotificationSuppressed || !generatedHistorical.DueAt.After(activation) {
		t.Fatalf("generated historical record=%+v err=%v", generatedHistorical, err)
	}
	var storedReminder repo.LeadReminderRecordModel
	if err := db.First(&storedReminder, legacyReminder.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedReminder.Status != types.ReminderStatusSuppressed {
		t.Fatalf("legacy follow-up reminder status=%s", storedReminder.Status)
	}
	rolloutSetting, err := settingRepo.GetByKey(ctx, runtime.SettingGroupRobotRulesRollout)
	if err != nil || rolloutSetting == nil || rolloutSetting.Value != activation.Format(time.RFC3339Nano) {
		t.Fatalf("rollout setting=%+v err=%v", rolloutSetting, err)
	}

	// Enabling after the first scan and restarting the worker must keep the
	// original boundary: old failed work stays suppressed, while a rule event
	// genuinely due after rollout is delivered.
	upsertRuntimeSetting(runtime.SettingDispatchClaimNotify, "true")
	if _, err := notifications.Create(ctx, &types.GroupRobotNotification{
		EventKey: "legacy-failed-discovered-after-restart", Category: types.GroupRobotCategoryDispatchClaim,
		EventType: types.GroupRobotEventDispatchOverdue, LeadID: lead.ID, BusinessVersion: lead.Version,
		DueAt: legacyDue, Status: types.GroupRobotNotificationFailed, NextRetryAt: &futureRetry,
		CreatedAt: legacyCreatedAt, UpdatedAt: legacyCreatedAt,
	}); err != nil {
		t.Fatal(err)
	}
	misdatedPayload, _ := json.Marshal(groupRobotEventPayload{ReminderMinutes: 3})
	if _, err := notifications.Create(ctx, &types.GroupRobotNotification{
		EventKey: "legacy-source-with-post-rollout-ledger-time", Category: types.GroupRobotCategoryDispatchClaim,
		EventType: types.GroupRobotEventDispatchOverdue, LeadID: lead.ID, BusinessVersion: lead.Version,
		DueAt: restartAt.Add(-time.Minute), Status: types.GroupRobotNotificationPending, PayloadJSON: string(misdatedPayload),
		CreatedAt: activation.Add(10 * time.Minute), UpdatedAt: activation.Add(10 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	secondSender := &recordingGroupRobotSender{}
	secondHandler := NewGroupRobotRuleHandler(
		leads, reminders, notifications, nil,
		runtime.NewDatabaseRuntimeSettings(settingRepo, 0), secondSender, nil, "https://crm.example.com",
	)
	secondHandler.rolloutInitAt = restartAt
	secondHandler.now = func() time.Time { return restartAt }
	if err := secondHandler.HandleScan(ctx, nil); err != nil {
		t.Fatal(err)
	}
	postEventKey := "dispatch:" + strconv.FormatInt(postLead.ID, 10) + ":v1:m3"
	postRecord, err := notifications.GetByEventKey(ctx, postEventKey)
	if err != nil || postRecord.Status != types.GroupRobotNotificationSent || len(secondSender.contents) != 1 {
		t.Fatalf("post-rollout record=%+v sends=%d err=%v", postRecord, len(secondSender.contents), err)
	}
	lateLegacyRecord, err := notifications.GetByEventKey(ctx, "legacy-failed-discovered-after-restart")
	if err != nil || lateLegacyRecord.Status != types.GroupRobotNotificationSuppressed {
		t.Fatalf("late legacy record=%+v err=%v", lateLegacyRecord, err)
	}
	misdatedLegacyRecord, err := notifications.GetByEventKey(ctx, "legacy-source-with-post-rollout-ledger-time")
	if err != nil || misdatedLegacyRecord.Status != types.GroupRobotNotificationSuppressed {
		t.Fatalf("misdated legacy record=%+v err=%v", misdatedLegacyRecord, err)
	}
	rolloutSetting, err = settingRepo.GetByKey(ctx, runtime.SettingGroupRobotRulesRollout)
	if err != nil || rolloutSetting.Value != activation.Format(time.RFC3339Nano) {
		t.Fatalf("rollout cutoff moved on restart: setting=%+v err=%v", rolloutSetting, err)
	}
}

func TestGroupRobotRuleHandlerRetriesAfterOneFiveAndFifteenMinutes(t *testing.T) {
	_, leads, notifications := newGroupRobotHandlerTestRepositories(t)
	clock := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	lead := groupRobotTestLead(clock)
	if err := leads.Create(context.Background(), lead); err != nil {
		t.Fatal(err)
	}
	_, err := notifications.Create(context.Background(), &types.GroupRobotNotification{
		EventKey: "retry", Category: types.GroupRobotCategoryNewLead, EventType: types.GroupRobotEventNewLead,
		LeadID: lead.ID, BusinessVersion: lead.Version, DueAt: clock,
		Status: types.GroupRobotNotificationPending, CreatedAt: clock, UpdatedAt: clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	settings := newGroupRobotHandlerSettings()
	settings.categories[types.GroupRobotCategoryNewLead] = true
	sender := &recordingGroupRobotSender{err: errors.New("webhook unavailable")}
	handler := NewGroupRobotRuleHandler(leads, nil, notifications, nil, settings, sender, nil, "https://crm.example.com")
	handler.now = func() time.Time { return clock }
	for attempt, delay := range []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, 0} {
		if err := handler.deliverReady(context.Background(), clock); err != nil {
			t.Fatal(err)
		}
		record, err := notifications.GetByEventKey(context.Background(), "retry")
		if err != nil {
			t.Fatal(err)
		}
		if record.Attempts != attempt+1 || record.Status != types.GroupRobotNotificationFailed {
			t.Fatalf("attempt %d record=%+v", attempt+1, record)
		}
		if delay == 0 {
			if record.NextRetryAt != nil {
				t.Fatalf("terminal failure retained retry time: %v", record.NextRetryAt)
			}
			continue
		}
		want := clock.Add(delay)
		if record.NextRetryAt == nil || !record.NextRetryAt.Equal(want) {
			t.Fatalf("attempt %d retry=%v, want %v", attempt+1, record.NextRetryAt, want)
		}
		clock = want
	}
	if len(sender.contents) != 4 {
		t.Fatalf("delivery attempts = %d, want 4", len(sender.contents))
	}
}

func TestGroupRobotRuleHandlerBridgesFollowupReminderToGroupRobot(t *testing.T) {
	db, leads, notifications := newGroupRobotHandlerTestRepositories(t)
	if err := db.AutoMigrate(&repo.LeadReminderRecordModel{}); err != nil {
		t.Fatal(err)
	}
	reminders := repo.NewGormLeadReminderRepository(db)
	now := time.Now().Add(-time.Minute).Truncate(time.Second)
	dueAt := now.Add(-time.Minute)
	lead := groupRobotTestLead(now.Add(-time.Hour))
	lead.Status = types.LeadStatusFollowing
	lead.Version = 2
	lead.BusinessStage = types.BusinessStageInvitation
	lead.OwnershipState = types.OwnershipPrivate
	lead.ResultStatus = types.LeadResultActive
	lead.CurrentOwnerUserID = "inviter-1"
	lead.AssignedInviterUserID = "inviter-1"
	lead.StarRating = 4
	lead.NextFollowupDueAt = &dueAt
	if err := leads.Create(context.Background(), lead); err != nil {
		t.Fatal(err)
	}
	record := &types.LeadReminderRecord{
		LeadID: lead.ID, ReminderType: types.ReminderTypeFollowupDue,
		BusinessVersion: lead.Version, DueAt: dueAt, ReceiverUserID: "inviter-1|manager-1",
		Status: types.ReminderStatusPending, IdempotencyKey: "followup-bridge", CreatedAt: now, UpdatedAt: now,
	}
	if _, err := reminders.Create(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	settings := newGroupRobotHandlerSettings()
	settings.categories[types.GroupRobotCategoryFollowup] = true
	sender := &recordingGroupRobotSender{}
	handler := NewGroupRobotRuleHandler(leads, reminders, notifications, nil, settings, sender, nil, "https://crm.example.com")
	handler.rolloutCutoff = dueAt.Add(-time.Hour)
	if err := handler.bridgeFollowupReminders(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if err := handler.deliverReady(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	notification, err := notifications.GetByEventKey(context.Background(), "followup-reminder:"+strconv.FormatInt(record.ID, 10))
	if err != nil || notification == nil || notification.Status != types.GroupRobotNotificationSent {
		t.Fatalf("notification=%+v err=%v", notification, err)
	}
	var stored repo.LeadReminderRecordModel
	if err := db.First(&stored, record.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != types.ReminderStatusSent || len(sender.contents) != 1 {
		t.Fatalf("reminder status=%s sends=%d", stored.Status, len(sender.contents))
	}
}

func newGroupRobotHandlerTestRepositories(t *testing.T) (*gorm.DB, *repo.GormLeadRepository, *repo.GormGroupRobotNotificationRepository) {
	t.Helper()
	db, err := repo.NewDB(&config.DatabaseConfig{
		Driver: "sqlite", Database: filepath.Join(t.TempDir(), "group-robot.db"), MaxConns: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&repo.LeadModel{}, &repo.LeadPoolRecordModel{}, &repo.GroupRobotNotificationModel{}); err != nil {
		t.Fatal(err)
	}
	return db, repo.NewGormLeadRepository(db), repo.NewGormGroupRobotNotificationRepository(db)
}

func groupRobotTestLead(now time.Time) *types.Lead {
	return &types.Lead{
		LeadNo: "GR-1", CustomerName: "测试客户", CustomerMobileMasked: "138****8000",
		CustomerWechatMasked: "wx****01", SourceChannel: "manual", ProductName: "课程",
		Status: types.LeadStatusPendingDispatch, Version: 1, DispatchManagerUserID: "manager-1",
		CreatedAt: now, UpdatedAt: now,
	}
}
