package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/security"
	"github.com/lsaac128/kxone/pkg/types"
)

func TestUpdateLeadProfilePersistsSecurelyAuditsAndKeepsBusinessState(t *testing.T) {
	db := newStage3TestDB(t)
	protector, err := security.NewAESGCMFieldProtector("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	hasher, err := security.NewHMACMobileHasher("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	leads := repo.NewGormLeadRepository(db).WithFieldProtector(protector).WithMobileHasher(hasher)
	activities := repo.NewGormLeadActivityRepository(db)
	tx := repo.NewGormTransactionManager(db).WithLeadSecurity(protector, hasher)
	uc := NewUpdateLeadProfileUseCase(leads, activities).WithTransactionManager(tx)

	now := time.Now()
	lead := &types.Lead{
		LeadNo: "PROFILE-1", CustomerName: "旧姓名", CustomerMobile: "13912345678", CustomerMobileMasked: "139****5678",
		City: "旧城市", ProductName: "旧项目", CustomerWechat: "wx-existing", CustomerWechatMasked: "wx****ng",
		Status: types.LeadStatusFollowing, Version: 7, BusinessStage: types.BusinessStageInvitation,
		OwnershipState: types.OwnershipPrivate, ResultStatus: types.LeadResultActive,
		InviterUserID: "inviter-1", CurrentOwnerUserID: "inviter-1", CurrentOwnerRole: types.OwnerRoleInviter,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := leads.Create(context.Background(), lead); err != nil {
		t.Fatal(err)
	}
	duplicate := &types.Lead{
		LeadNo: "PROFILE-DUP", CustomerName: "重复号码", CustomerMobile: "13800138000", CustomerMobileMasked: "138****8000",
		Status: types.LeadStatusFollowing, Version: 1, BusinessStage: types.BusinessStageInvitation,
		OwnershipState: types.OwnershipPrivate, ResultStatus: types.LeadResultActive, CreatedAt: now, UpdatedAt: now,
	}
	if err := leads.Create(context.Background(), duplicate); err != nil {
		t.Fatal(err)
	}
	reminder := &repo.LeadReminderRecordModel{
		LeadID: lead.ID, ReminderType: types.ReminderTypeFollowupDue, BusinessVersion: lead.Version,
		DueAt: now.Add(time.Hour), ReceiverUserID: "inviter-1", Status: types.ReminderStatusPending,
		IdempotencyKey: "profile-edit-reminder", CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(reminder).Error; err != nil {
		t.Fatal(err)
	}
	notification := &repo.GroupRobotNotificationModel{
		EventKey: "profile-edit-notification", Category: types.GroupRobotCategoryFollowup,
		EventType: types.GroupRobotEventFollowupDue, LeadID: lead.ID, BusinessVersion: lead.Version,
		DueAt: now.Add(time.Hour), Status: types.GroupRobotNotificationPending, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(notification).Error; err != nil {
		t.Fatal(err)
	}

	staleBusinessCopy, err := leads.GetByID(context.Background(), lead.ID)
	if err != nil {
		t.Fatal(err)
	}
	name, mobile, city, product := "新姓名", "+86 138-0013-8000", "武汉", "高考复读"
	result, err := uc.Execute(context.Background(), &UpdateLeadProfileRequest{
		LeadID: lead.ID, ActorUserID: "manager-1", ActorRole: string(types.UserRoleManager),
		ExpectedBusinessVersion: lead.Version, ExpectedProfileVersion: lead.ProfileVersion,
		CustomerName: &name, CustomerMobile: &mobile, City: &city, ProductName: &product,
	})
	if err != nil {
		t.Fatalf("update profile: %v", err)
	}
	if result.Lead.Version != 7 || result.Lead.ProfileVersion != 2 {
		t.Fatalf("versions = business:%d profile:%d, want 7/2", result.Lead.Version, result.Lead.ProfileVersion)
	}
	if len(result.Warnings) != 1 || result.Warnings[0] != LeadProfileWarningDuplicateMobile {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
	got, err := leads.GetByID(context.Background(), lead.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CustomerName != name || got.CustomerMobile != "13800138000" || got.CustomerMobileMasked != "138****8000" || got.City != city || got.ProductName != product {
		t.Fatalf("updated lead = %+v", got)
	}
	if got.Status != types.LeadStatusFollowing || got.NextFollowupDueAt != lead.NextFollowupDueAt || got.Version != lead.Version {
		t.Fatalf("business state changed = %+v", got)
	}
	var storedReminder repo.LeadReminderRecordModel
	if err := db.First(&storedReminder, reminder.ID).Error; err != nil || storedReminder.Status != types.ReminderStatusPending || storedReminder.BusinessVersion != lead.Version {
		t.Fatalf("reminder changed = %+v err=%v", storedReminder, err)
	}
	var storedNotification repo.GroupRobotNotificationModel
	if err := db.First(&storedNotification, notification.ID).Error; err != nil || storedNotification.Status != types.GroupRobotNotificationPending || storedNotification.BusinessVersion != lead.Version {
		t.Fatalf("group notification changed = %+v err=%v", storedNotification, err)
	}
	var stored repo.LeadModel
	if err := db.First(&stored, lead.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !security.IsEncryptedField(stored.CustomerMobile) || stored.CustomerMobile == got.CustomerMobile || stored.CustomerMobileHash == "" {
		t.Fatalf("stored contact is not protected: mobile=%q hash=%q", stored.CustomerMobile, stored.CustomerMobileHash)
	}
	records, err := activities.ListByLead(context.Background(), lead.ID)
	if err != nil || len(records) != 1 || records[0].ActivityType != types.LeadActivityProfileUpdated {
		t.Fatalf("activities = %+v err=%v", records, err)
	}
	audit := records[0].FromValueJSON + records[0].ToValueJSON + records[0].MetadataJSON
	if strings.Contains(audit, "13912345678") || strings.Contains(audit, "13800138000") || !strings.Contains(audit, "138****8000") {
		t.Fatalf("unsafe audit payload = %s", audit)
	}

	staleBusinessCopy.Version++
	if err := leads.Update(context.Background(), staleBusinessCopy); !errors.Is(err, types.ErrLeadVersionConflict) {
		t.Fatalf("stale business update err=%v, want version conflict", err)
	}
	_, err = uc.Execute(context.Background(), &UpdateLeadProfileRequest{
		LeadID: lead.ID, ActorUserID: "manager-1", ActorRole: string(types.UserRoleManager),
		ExpectedBusinessVersion: got.Version, ExpectedProfileVersion: 1, City: &city,
	})
	if !errors.Is(err, types.ErrLeadVersionConflict) {
		t.Fatalf("stale profile update err=%v, want version conflict", err)
	}
}

func TestLeadProfileEditPermissionMatrix(t *testing.T) {
	activeInvitation := &types.Lead{Status: types.LeadStatusFollowing, BusinessStage: types.BusinessStageInvitation, ResultStatus: types.LeadResultActive, CurrentOwnerUserID: "inviter-1", InviterUserID: "inviter-1"}
	activeNegotiation := &types.Lead{Status: types.LeadStatusFollowing, BusinessStage: types.BusinessStageNegotiation, ResultStatus: types.LeadResultActive, CurrentOwnerUserID: "closer-1", CloserUserID: "closer-1"}
	closed := &types.Lead{Status: types.LeadStatusClosed, BusinessStage: types.BusinessStageFinished, ResultStatus: types.LeadResultClosed}
	tests := []struct {
		name string
		lead *types.Lead
		user string
		role types.UserRole
		want bool
	}{
		{"admin any", closed, "admin-1", types.UserRoleAdmin, true},
		{"manager any", closed, "manager-1", types.UserRoleManager, true},
		{"inviter own active", activeInvitation, "inviter-1", types.UserRoleInviter, true},
		{"inviter other", activeInvitation, "inviter-2", types.UserRoleInviter, false},
		{"closer own active", activeNegotiation, "closer-1", types.UserRoleCloser, true},
		{"closer invitation", activeInvitation, "closer-1", types.UserRoleCloser, false},
		{"dispatcher read only", activeInvitation, "dispatcher-1", types.UserRoleDispatcher, false},
		{"inviter terminal", closed, "inviter-1", types.UserRoleInviter, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canEditLeadProfile(tt.lead, tt.user, tt.role); got != tt.want {
				t.Fatalf("canEditLeadProfile()=%v want=%v", got, tt.want)
			}
		})
	}
}

func TestApplyLeadProfilePatchValidatesContactFallback(t *testing.T) {
	lead := &types.Lead{CustomerName: "客户", CustomerMobile: "13912345678"}
	empty := ""
	if err := applyLeadProfilePatch(lead, &UpdateLeadProfileRequest{CustomerMobile: &empty}); !errors.Is(err, types.ErrInvalidLeadProfile) {
		t.Fatalf("clear only contact err=%v", err)
	}
	lead.CustomerWechat = "wx-id"
	if err := applyLeadProfilePatch(lead, &UpdateLeadProfileRequest{CustomerMobile: &empty}); err != nil {
		t.Fatalf("clear mobile with wechat: %v", err)
	}
	invalid := "138(0013)8000"
	if err := applyLeadProfilePatch(lead, &UpdateLeadProfileRequest{CustomerMobile: &invalid}); !errors.Is(err, types.ErrInvalidLeadProfile) {
		t.Fatalf("invalid phone err=%v", err)
	}
}
