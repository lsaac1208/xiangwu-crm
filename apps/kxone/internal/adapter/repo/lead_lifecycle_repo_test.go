package repo

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/config"
	"github.com/lsaac128/kxone/pkg/types"
)

func TestLeadLifecycleDeleteAndRestore(t *testing.T) {
	db, err := NewDB(&config.DatabaseConfig{Driver: "sqlite", Database: filepath.Join(t.TempDir(), "lifecycle.db"), MaxConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	// Exercise the versioned SQL schema used in production. AutoMigrate would
	// silently accept a mismatched GORM column name here.
	if err := RunMigrations(db, "../../../migrations"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	lead := &LeadModel{LeadNo: "xiangwu_lead_form:test", CustomerName: "测试", SourceChannel: "独立咨询表单", Status: string(types.LeadStatusPendingDispatch), Version: 2, ProfileVersion: 3, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(lead).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&LeadReminderRecordModel{LeadID: lead.ID, ReminderType: types.ReminderTypeFollowupDue, IdempotencyKey: "lifecycle-reminder", Status: types.ReminderStatusPending, DueAt: now, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&GroupRobotNotificationModel{LeadID: lead.ID, EventKey: "lifecycle-robot", EventType: types.GroupRobotEventNewLead, Category: types.GroupRobotCategoryNewLead, Status: types.GroupRobotNotificationPending, DueAt: now, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&XiangwuFormSubmission{LeadID: lead.ID, Source: "xiangwu_lead_form", ExternalLeadID: "12345678-1234-4123-8123-123456789abc", PayloadHash: "hash", Details: "阶段：高三", SourcePath: "/form/", CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	repository := NewGormLeadLifecycleRepository(db, nil)
	ctx := context.Background()
	if err := repository.Delete(ctx, lead.ID, lead.LeadNo, 1, 3, "admin", "清理测试"); !errors.Is(err, ErrLeadLifecycleConflict) {
		t.Fatalf("stale version: %v", err)
	}
	if err := repository.Delete(ctx, lead.ID, lead.LeadNo, 2, 3, "admin", "清理测试"); err != nil {
		t.Fatal(err)
	}
	if err := repository.Delete(ctx, lead.ID, lead.LeadNo, 2, 3, "admin", "重复删除"); !errors.Is(err, ErrLeadLifecycleNotFound) {
		t.Fatalf("duplicate delete: %v", err)
	}
	visible, err := NewGormLeadRepository(db).GetByID(ctx, lead.ID)
	if err != nil || visible != nil {
		t.Fatalf("deleted lead remained visible: %+v %v", visible, err)
	}
	items, total, err := repository.ListDeleted(ctx, 0, 20)
	if err != nil || total != 1 || len(items) != 1 || items[0].Reason != "清理测试" || items[0].DeletedBy != "admin" {
		t.Fatalf("recycle bin: %+v %d %v", items, total, err)
	}
	var reminder LeadReminderRecordModel
	if err := db.First(&reminder, "lead_id = ?", lead.ID).Error; err != nil || reminder.Status != types.ReminderStatusCancelled {
		t.Fatalf("reminder not cancelled: %+v %v", reminder, err)
	}
	var notification GroupRobotNotificationModel
	if err := db.First(&notification, "lead_id = ?", lead.ID).Error; err != nil || notification.Status != types.GroupRobotNotificationCancelled {
		t.Fatalf("notification not cancelled: %+v %v", notification, err)
	}
	var form XiangwuFormSubmission
	if err := db.First(&form, "lead_id = ?", lead.ID).Error; err != nil {
		t.Fatalf("form idempotency record lost: %v", err)
	}
	if err := repository.Restore(ctx, lead.ID, "admin", "误删恢复"); err != nil {
		t.Fatal(err)
	}
	visible, err = NewGormLeadRepository(db).GetByID(ctx, lead.ID)
	if err != nil || visible == nil || visible.Version != 4 || visible.ProfileVersion != 5 {
		t.Fatalf("restore failed: %+v %v", visible, err)
	}
	_, total, err = repository.ListDeleted(ctx, 0, 20)
	if err != nil || total != 0 {
		t.Fatalf("recycle bin after restore: %d %v", total, err)
	}
	var events []LeadDeletionEventModel
	if err := db.Order("id").Find(&events).Error; err != nil || len(events) != 2 || events[0].Action != "delete" || events[1].Action != "restore" {
		t.Fatalf("audit events: %+v %v", events, err)
	}
	if err := repository.Restore(ctx, lead.ID, "admin", "重复恢复"); !errors.Is(err, ErrLeadLifecycleConflict) {
		t.Fatalf("duplicate restore: %v", err)
	}
}
