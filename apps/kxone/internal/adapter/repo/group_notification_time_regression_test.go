package repo

import (
	"github.com/lsaac128/kxone/pkg/types"
	"testing"
	"time"
)

func TestGroupNotificationDueTimeUsesAbsoluteInstant(t *testing.T) {
	db := newMigrationTestDB(t)
	if err := db.AutoMigrate(&GroupRobotNotificationModel{}); err != nil {
		t.Fatal(err)
	}
	r := NewGormGroupRobotNotificationRepository(db)
	dueUTC := time.Date(2026, 10, 25, 1, 0, 0, 0, time.UTC)
	dueShanghai := dueUTC.In(types.BusinessLocation)
	now := dueUTC.Add(time.Minute)
	for _, x := range []struct {
		key string
		due time.Time
	}{{"same-instant-utc", dueUTC}, {"same-instant-shanghai", dueShanghai}} {
		_, err := r.Create(t.Context(), &types.GroupRobotNotification{EventKey: x.key, Category: types.GroupRobotCategoryFollowup, EventType: types.GroupRobotEventFollowupDue, LeadID: 1, DueAt: x.due, Status: types.GroupRobotNotificationPending, CreatedAt: dueUTC.Add(-time.Hour), UpdatedAt: dueUTC})
		if err != nil {
			t.Fatal(err)
		}
	}
	// Simulate pre-normalization records: new writes alone cannot test legacy offsets.
	if err := db.Model(&GroupRobotNotificationModel{}).Where("event_key = ?", "same-instant-shanghai").Update("due_at", dueShanghai).Error; err != nil {
		t.Fatal(err)
	}
	rows, err := r.ListReady(t.Context(), now, 10)
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{}
	for _, x := range rows {
		keys = append(keys, x.EventKey)
	}
	t.Logf("same instant due: UTC=%s Shanghai=%s; scan=%s; ready=%v", dueUTC.Format(time.RFC3339), dueShanghai.Format(time.RFC3339), now.Format(time.RFC3339), keys)
	if len(rows) != 2 {
		t.Errorf("BUG: both notifications are overdue but only %d is selected", len(rows))
	}
	later, err := r.ListReady(t.Context(), now.Add(8*time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("8 hours later: ready=%d", len(later))
}

func TestNotificationSuppressionPreservesFutureUTCRecords(t *testing.T) {
	db := newMigrationTestDB(t)
	if err := db.AutoMigrate(&GroupRobotNotificationModel{}); err != nil {
		t.Fatal(err)
	}
	r := NewGormGroupRobotNotificationRepository(db)
	now := time.Date(2026, 10, 26, 2, 0, 0, 0, time.UTC)
	for _, x := range []struct {
		key string
		due time.Time
	}{{"past", now.Add(-time.Hour)}, {"future", now.Add(time.Hour)}} {
		_, err := r.Create(t.Context(), &types.GroupRobotNotification{EventKey: x.key, Category: types.GroupRobotCategoryFollowup, EventType: types.GroupRobotEventFollowupDue, LeadID: 1, DueAt: x.due, Status: types.GroupRobotNotificationPending, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			t.Fatal(err)
		}
	}
	n, err := r.SuppressDueCategory(t.Context(), types.GroupRobotCategoryFollowup, now.In(types.BusinessLocation), "test enable boundary")
	if err != nil || n != 1 {
		t.Fatalf("suppressed=%d want1, err=%v", n, err)
	}
	future, err := r.GetByEventKey(t.Context(), "future")
	if err != nil || future.Status != types.GroupRobotNotificationPending {
		t.Fatalf("future delivery suppressed: %+v %v", future, err)
	}
}

func TestNotificationRetryAndClaimUseAbsoluteInstant(t *testing.T) {
	db := newMigrationTestDB(t)
	if err := db.AutoMigrate(&GroupRobotNotificationModel{}); err != nil {
		t.Fatal(err)
	}
	r := NewGormGroupRobotNotificationRepository(db)
	now := time.Now().UTC()
	past := now.Add(-time.Minute).In(types.BusinessLocation)
	_, err := r.Create(t.Context(), &types.GroupRobotNotification{EventKey: "legacy-retry", Category: types.GroupRobotCategoryFollowup, EventType: types.GroupRobotEventFollowupDue, LeadID: 1, DueAt: past, Status: types.GroupRobotNotificationFailed, NextRetryAt: &past, CreatedAt: past, UpdatedAt: past})
	if err != nil {
		t.Fatal(err)
	}
	// A legacy row deliberately keeps its original offset in both columns.
	if err := db.Model(&GroupRobotNotificationModel{}).Where("event_key = ?", "legacy-retry").Updates(map[string]any{"due_at": past, "next_retry_at": past}).Error; err != nil {
		t.Fatal(err)
	}
	rows, err := r.ListReady(t.Context(), now, 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("retry not ready: %d %v", len(rows), err)
	}
	ok, err := r.TryMarkSending(t.Context(), rows[0].ID)
	if err != nil || !ok {
		t.Fatalf("ready retry not claimable: %v %v", ok, err)
	}
}
