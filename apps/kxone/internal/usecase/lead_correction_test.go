package usecase

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/pkg/types"
)

func TestPaymentCorrectionsPersistAndAudit(t *testing.T) {
	db := newStage3TestDB(t)
	leads := repo.NewGormLeadRepository(db)
	activities := repo.NewGormLeadActivityRepository(db)
	uc := NewCompleteLeadUseCase(leads).WithAudit(activities, repo.NewGormTransactionManager(db))
	current := stage3OwnedLead(1, "inviter-1")
	if err := leads.Create(t.Context(), current); err != nil {
		t.Fatal(err)
	}
	result, err := uc.Execute(t.Context(), &CompleteLeadRequest{LeadID: 1, ActorUserID: "inviter-1", ActorRole: "inviter", Action: "convert", DealAmount: 1000, PaymentStatus: "partial"})
	if err != nil {
		t.Fatal(err)
	}
	convertedAt := *result.ConvertedAt
	for _, payment := range []struct {
		status         string
		amount, refund float64
	}{{"partial", 1200.25, 0}, {"paid", 8000, 0}, {"refunded", 8000, 8000}, {"paid", 8000, 0}} {
		version := result.Version
		result, err = uc.Execute(t.Context(), &CompleteLeadRequest{LeadID: 1, ActorUserID: "inviter-1", ActorRole: "inviter", Action: "update_deal", ExpectedVersion: &version, Reason: "核对收款", DealAmount: payment.amount, PaymentStatus: payment.status, RefundAmount: payment.refund})
		if err != nil {
			t.Fatal(err)
		}
		persisted, _ := leads.GetByID(t.Context(), 1)
		if persisted.PaymentStatus != payment.status || persisted.DealAmount != payment.amount || persisted.RefundAmount != payment.refund || !persisted.ConvertedAt.Equal(convertedAt) {
			t.Fatalf("persisted = %+v", persisted)
		}
	}
	records, err := activities.ListByLead(t.Context(), 1)
	if err != nil || len(records) != 5 {
		t.Fatalf("audit count = %d, err=%v", len(records), err)
	}
	stale := result.Version - 1
	_, err = uc.Execute(t.Context(), &CompleteLeadRequest{LeadID: 1, ActorUserID: "inviter-1", ActorRole: "inviter", Action: "update_deal", ExpectedVersion: &stale, Reason: "旧窗口", DealAmount: 9, PaymentStatus: "paid"})
	if !errors.Is(err, types.ErrLeadVersionConflict) {
		t.Fatalf("stale error=%v", err)
	}
	persisted, _ := leads.GetByID(t.Context(), 1)
	if persisted.DealAmount != 8000 {
		t.Fatal("stale write changed amount")
	}
}

func TestCorrectionPermissionsAndRestoration(t *testing.T) {
	for _, stage := range []types.BusinessStage{types.BusinessStageInvitation, types.BusinessStageNegotiation} {
		for _, actor := range []struct {
			role, id string
			allowed  bool
		}{{"admin", "admin", true}, {"manager", "manager", true}, {"inviter", "inviter-1", stage == types.BusinessStageInvitation}, {"closer", "closer-1", stage == types.BusinessStageNegotiation}, {"inviter", "other", false}, {"closer", "other", false}, {"dispatcher", "dispatcher", false}} {
			t.Run(string(stage)+"/"+actor.role+"/"+actor.id, func(t *testing.T) {
				leads := NewMockLeadRepository()
				current := stage3OwnedLead(1, "inviter-1")
				current.Status = types.LeadStatusInvalid
				current.ResultStatus = types.LeadResultInvalid
				current.BusinessStage = types.BusinessStageFinished
				if stage == types.BusinessStageNegotiation {
					current.CurrentOwnerRole = types.OwnerRoleCloser
					current.CurrentOwnerUserID = "closer-1"
					current.CloserUserID = "closer-1"
					now := time.Now()
					current.DeliveredAt = &now
				}
				leads.leads[1] = current
				version := current.Version
				got, err := NewCompleteLeadUseCase(leads).Execute(t.Context(), &CompleteLeadRequest{LeadID: 1, ActorRole: actor.role, ActorUserID: actor.id, Action: "restore_invalid", ExpectedVersion: &version, Reason: "误标"})
				if !actor.allowed {
					if !errors.Is(err, types.ErrUnauthorized) {
						t.Fatalf("err=%v", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if got.Status != types.LeadStatusFollowing || got.BusinessStage != stage || got.ResultStatus != types.LeadResultActive || got.NextFollowupDueAt != nil {
					t.Fatalf("restored=%+v", got)
				}
				if !canEditLeadProfile(got, actor.id, types.UserRole(actor.role)) {
					t.Fatal("restored profile inaccessible")
				}
			})
		}
	}
}

func TestCorrectionRejectsInvalidPayments(t *testing.T) {
	for _, test := range []struct {
		status         string
		amount, refund float64
	}{{"bogus", 1, 0}, {"paid", 0, 0}, {"paid", 1.001, 0}, {"paid", math.NaN(), 0}, {"paid", math.Inf(1), 0}, {"paid", 1, 1}, {"refunded", 1, 2}, {"refunded", 1, 0}, {"refunded", 1, -1}, {"paid", 1000000000, 0}} {
		current := &types.Lead{}
		err := applyPayment(current, &CompleteLeadRequest{PaymentStatus: test.status, DealAmount: test.amount, RefundAmount: test.refund})
		if !errors.Is(err, types.ErrLeadStatusInvalid) {
			t.Errorf("%+v: err=%v", test, err)
		}
	}
}

func TestCorrectionRollsBackWhenAuditFails(t *testing.T) {
	db := newStage3TestDB(t)
	leads := repo.NewGormLeadRepository(db)
	current := stage3OwnedLead(1, "inviter-1")
	current.Status = types.LeadStatusConverted
	current.BusinessStage = types.BusinessStageFinished
	current.ResultStatus = types.LeadResultConverted
	current.DealAmount = 100
	current.PaymentStatus = "partial"
	if err := leads.Create(t.Context(), current); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropTable(&repo.LeadActivityRecordModel{}); err != nil {
		t.Fatal(err)
	}
	version := current.Version
	_, err := NewCompleteLeadUseCase(leads).WithAudit(repo.NewGormLeadActivityRepository(db), repo.NewGormTransactionManager(db)).Execute(context.Background(), &CompleteLeadRequest{LeadID: 1, ActorRole: "inviter", ActorUserID: "inviter-1", Action: "update_deal", ExpectedVersion: &version, Reason: "补款", PaymentStatus: "paid", DealAmount: 200})
	if err == nil {
		t.Fatal("expected audit failure")
	}
	got, _ := leads.GetByID(t.Context(), 1)
	if got.DealAmount != 100 || got.Version != version {
		t.Fatal("failed audit did not roll back")
	}
}

func TestRefundDeductedFromDashboardAmounts(t *testing.T) {
	leads := []*types.Lead{{Status: types.LeadStatusConverted, DealAmount: 1000, RefundAmount: 1000}, {Status: types.LeadStatusConverted, DealAmount: 2000, RefundAmount: 500}}
	overview := buildDashboardOverview(leads, overdueThresholds{})
	business := buildBusinessStats(leads)
	if overview.TotalDealAmount != 1500 || business.TotalDealAmount != 1500 {
		t.Fatalf("overview=%+v business=%+v", overview, business)
	}
}

func TestCloserCanAccessAndCorrectOwnTerminalLead(t *testing.T) {
	db := newStage3TestDB(t)
	leads := repo.NewGormLeadRepository(db)
	for _, status := range []types.LeadStatus{types.LeadStatusConverted, types.LeadStatusInvalid} {
		current := stage3OwnedLead(0, "inviter-1")
		current.LeadNo = "TERMINAL-" + string(status)
		current.Status = status
		current.BusinessStage = types.BusinessStageFinished
		current.ResultStatus = types.LeadResultStatus(status)
		current.CurrentOwnerRole = types.OwnerRoleCloser
		current.CurrentOwnerUserID = "closer-1"
		current.CloserUserID = "closer-1"
		if err := leads.Create(t.Context(), current); err != nil {
			t.Fatal(err)
		}
		uc := NewGetLeadUseCase(leads, repo.NewGormFollowupRepository(db), repo.NewGormDispatchRecordRepository(db))
		got, err := uc.Execute(t.Context(), current.ID, "closer-1", "closer")
		if err != nil {
			t.Fatal(err)
		}
		if got.Lead.CustomerMobile == "" || !canEditLeadProfile(got.Lead, "closer-1", types.UserRoleCloser) {
			t.Fatal("terminal lead not editable")
		}
		_, err = uc.Execute(t.Context(), current.ID, "closer-2", "closer")
		if !errors.Is(err, types.ErrUnauthorized) {
			t.Fatalf("other closer err=%v", err)
		}
	}
	listed, total, err := leads.ListByCloserFiltered(t.Context(), "closer-1", repo.LeadListFilter{}, 0, 20)
	if err != nil || total != 2 || len(listed) != 2 {
		t.Fatalf("list total=%d len=%d err=%v", total, len(listed), err)
	}
}
