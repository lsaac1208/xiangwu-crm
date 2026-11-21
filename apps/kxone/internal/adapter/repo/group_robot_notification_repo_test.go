package repo

import (
	"context"
	"testing"
	"time"

	"github.com/lsaac128/kxone/pkg/types"
)

func TestGroupRobotNotificationRepositoryDurableDeliveryLifecycle(t *testing.T) {
	db := newMigrationTestDB(t)
	if err := db.AutoMigrate(&GroupRobotNotificationModel{}); err != nil {
		t.Fatal(err)
	}
	repository := NewGormGroupRobotNotificationRepository(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	notification := &types.GroupRobotNotification{
		EventKey: "new-lead:10", Category: types.GroupRobotCategoryNewLead,
		EventType: types.GroupRobotEventNewLead, LeadID: 10, BusinessVersion: 1,
		DueAt: now.Add(-time.Minute), Status: types.GroupRobotNotificationPending,
		CreatedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute),
	}
	created, err := repository.Create(ctx, notification)
	if err != nil || !created {
		t.Fatalf("create=%v err=%v", created, err)
	}
	duplicate := *notification
	duplicate.ID = 0
	created, err = repository.Create(ctx, &duplicate)
	if err != nil || created || duplicate.ID != notification.ID {
		t.Fatalf("duplicate create=%v id=%d want=%d err=%v", created, duplicate.ID, notification.ID, err)
	}

	ready, err := repository.ListReady(ctx, now, 10)
	if err != nil || len(ready) != 1 {
		t.Fatalf("ready=%d err=%v", len(ready), err)
	}
	claimed, err := repository.TryMarkSending(ctx, notification.ID)
	if err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	claimed, err = repository.TryMarkSending(ctx, notification.ID)
	if err != nil || claimed {
		t.Fatalf("duplicate claim=%v err=%v", claimed, err)
	}

	retryAt := now.Add(time.Minute)
	if err := repository.MarkFailed(ctx, notification.ID, "temporary", &retryAt); err != nil {
		t.Fatal(err)
	}
	ready, err = repository.ListReady(ctx, retryAt.Add(-time.Second), 10)
	if err != nil || len(ready) != 0 {
		t.Fatalf("premature ready=%d err=%v", len(ready), err)
	}
	ready, err = repository.ListReady(ctx, retryAt, 10)
	if err != nil || len(ready) != 1 {
		t.Fatalf("retry ready=%d err=%v", len(ready), err)
	}

	if err := db.Model(&GroupRobotNotificationModel{}).Where("id = ?", notification.ID).Updates(map[string]any{
		"status": types.GroupRobotNotificationSending, "attempts": 2, "updated_at": now.Add(-10 * time.Minute),
	}).Error; err != nil {
		t.Fatal(err)
	}
	recovered, err := repository.RecoverStaleSending(ctx, now.Add(-5*time.Minute), now)
	if err != nil || recovered != 1 {
		t.Fatalf("recovered=%d err=%v", recovered, err)
	}
	record, err := repository.GetByEventKey(ctx, notification.EventKey)
	if err != nil || record.Status != types.GroupRobotNotificationFailed || record.NextRetryAt == nil || !record.NextRetryAt.Equal(now) {
		t.Fatalf("recovered record=%+v err=%v", record, err)
	}
}

func TestGroupRobotNotificationRepositorySuppressesDueCategoryOnly(t *testing.T) {
	db := newMigrationTestDB(t)
	if err := db.AutoMigrate(&GroupRobotNotificationModel{}); err != nil {
		t.Fatal(err)
	}
	repository := NewGormGroupRobotNotificationRepository(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	for _, item := range []struct {
		key      string
		category string
		dueAt    time.Time
	}{
		{key: "due-followup", category: types.GroupRobotCategoryFollowup, dueAt: now.Add(-time.Minute)},
		{key: "future-followup", category: types.GroupRobotCategoryFollowup, dueAt: now.Add(time.Minute)},
		{key: "due-pool", category: types.GroupRobotCategoryPool, dueAt: now.Add(-time.Minute)},
	} {
		_, err := repository.Create(ctx, &types.GroupRobotNotification{
			EventKey: item.key, Category: item.category, EventType: types.GroupRobotEventNewLead,
			LeadID: 1, DueAt: item.dueAt, Status: types.GroupRobotNotificationPending,
			CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	count, err := repository.SuppressDueCategory(ctx, types.GroupRobotCategoryFollowup, now, "disabled before enable")
	if err != nil || count != 1 {
		t.Fatalf("suppressed=%d err=%v", count, err)
	}
	for key, want := range map[string]string{
		"due-followup":    types.GroupRobotNotificationSuppressed,
		"future-followup": types.GroupRobotNotificationPending,
		"due-pool":        types.GroupRobotNotificationPending,
	} {
		record, err := repository.GetByEventKey(ctx, key)
		if err != nil || record.Status != want {
			t.Fatalf("%s status=%v want=%s err=%v", key, record, want, err)
		}
	}
}

func TestGroupRobotNotificationRepositoryListsOnlyMissingPostCutoffSourceEventsWithinLimit(t *testing.T) {
	db := newMigrationTestDB(t)
	if err := db.AutoMigrate(&LeadModel{}, &LeadPoolRecordModel{}, &GroupRobotNotificationModel{}); err != nil {
		t.Fatal(err)
	}
	repository := NewGormGroupRobotNotificationRepository(db)
	ctx := context.Background()
	cutoff := time.Now().Truncate(time.Second)

	createLead := func(leadNo string, createdAt time.Time, version int, status string) *LeadModel {
		t.Helper()
		model := &LeadModel{
			LeadNo: leadNo, CustomerName: leadNo, Status: status, Version: version,
			CreatedAt: createdAt, UpdatedAt: createdAt,
		}
		if err := db.WithContext(ctx).Create(model).Error; err != nil {
			t.Fatal(err)
		}
		return model
	}
	historical := createLead("SOURCE-HISTORICAL", cutoff.Add(-time.Second), 1, string(types.LeadStatusPendingDispatch))
	covered := createLead("SOURCE-COVERED", cutoff, 2, string(types.LeadStatusFollowing))
	missingFirst := createLead("SOURCE-MISSING-1", cutoff.Add(time.Second), 3, string(types.LeadStatusConverted))
	missingSecond := createLead("SOURCE-MISSING-2", cutoff.Add(2*time.Second), 4, string(types.LeadStatusPublicPool))
	if _, err := repository.Create(ctx, &types.GroupRobotNotification{
		EventKey: "new-lead-covered", Category: types.GroupRobotCategoryNewLead, EventType: types.GroupRobotEventNewLead,
		LeadID: covered.ID, BusinessVersion: covered.Version, DueAt: covered.CreatedAt,
		Status: types.GroupRobotNotificationPending, CreatedAt: covered.CreatedAt, UpdatedAt: covered.CreatedAt,
	}); err != nil {
		t.Fatal(err)
	}

	newLeadEvents, err := repository.ListMissingNewLeadEvents(ctx, cutoff, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(newLeadEvents) != 1 || newLeadEvents[0].LeadID != missingFirst.ID || newLeadEvents[0].BusinessVersion != missingFirst.Version {
		t.Fatalf("bounded missing new-lead events = %+v", newLeadEvents)
	}
	newLeadEvents, err = repository.ListMissingNewLeadEvents(ctx, cutoff, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(newLeadEvents) != 2 || newLeadEvents[0].LeadID != missingFirst.ID || newLeadEvents[1].LeadID != missingSecond.ID {
		t.Fatalf("missing new-lead events = %+v", newLeadEvents)
	}
	for _, event := range newLeadEvents {
		if event.LeadID == historical.ID || event.LeadID == covered.ID {
			t.Fatalf("historical or covered lead leaked into compensation: %+v", event)
		}
	}

	createPoolRecord := func(leadID int64, action, reason, actorUserID string, version int, createdAt time.Time) *LeadPoolRecordModel {
		t.Helper()
		model := &LeadPoolRecordModel{
			LeadID: leadID, Action: action, Reason: reason, PreviousOwnerUserID: "owner-1",
			ActorUserID: actorUserID, PoolVersion: version, CreatedAt: createdAt,
		}
		if err := db.WithContext(ctx).Create(model).Error; err != nil {
			t.Fatal(err)
		}
		return model
	}
	createPoolRecord(missingFirst.ID, types.PoolActionEnter, types.PoolReasonUnclaimedTimeout, "system", 10, cutoff.Add(-time.Second))
	createPoolRecord(missingFirst.ID, types.PoolActionEnter, types.PoolReasonManual, "manager-1", 11, cutoff.Add(time.Second))
	createPoolRecord(missingFirst.ID, types.PoolActionEnter, types.PoolReasonRecoverableInvalid, "system", 12, cutoff.Add(2*time.Second))
	createPoolRecord(missingFirst.ID, types.PoolActionClaim, types.PoolReasonFollowupOverdue, "system", 13, cutoff.Add(3*time.Second))
	createPoolRecord(missingFirst.ID, types.PoolActionEnter, types.PoolReasonFollowupOverdue, "manager-1", 14, cutoff.Add(4*time.Second))
	coveredPool := createPoolRecord(missingFirst.ID, types.PoolActionEnter, types.PoolReasonUnclaimedTimeout, "system", 15, cutoff.Add(5*time.Second))
	missingPool := createPoolRecord(missingSecond.ID, types.PoolActionEnter, types.PoolReasonFollowupOverdue, "system", 16, cutoff.Add(6*time.Second))
	if _, err := repository.Create(ctx, &types.GroupRobotNotification{
		EventKey: "covered-pool", Category: types.GroupRobotCategoryPool, EventType: types.GroupRobotEventPoolAutoReturn,
		LeadID: coveredPool.LeadID, BusinessVersion: coveredPool.PoolVersion, DueAt: coveredPool.CreatedAt,
		Status: types.GroupRobotNotificationPending, CreatedAt: coveredPool.CreatedAt, UpdatedAt: coveredPool.CreatedAt,
	}); err != nil {
		t.Fatal(err)
	}

	poolEvents, err := repository.ListMissingAutoPoolEvents(ctx, cutoff, 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(poolEvents) != 1 || poolEvents[0].LeadID != missingPool.LeadID ||
		poolEvents[0].BusinessVersion != missingPool.PoolVersion || poolEvents[0].Reason != types.PoolReasonFollowupOverdue ||
		poolEvents[0].PreviousOwnerUserID != "owner-1" || poolEvents[0].ActorUserID != "system" {
		t.Fatalf("missing automatic pool events = %+v", poolEvents)
	}
}
