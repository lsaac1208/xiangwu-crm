package usecase

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/pkg/types"
)

type stage4SettingsStub struct{}

func (stage4SettingsStub) WorkflowV2Enabled(context.Context) bool     { return true }
func (stage4SettingsStub) AppointmentEnabled(context.Context) bool    { return true }
func (stage4SettingsStub) PoolEnabled(context.Context) bool           { return true }
func (stage4SettingsStub) PoolAutoReturnEnabled(context.Context) bool { return false }
func (stage4SettingsStub) AppointmentDefaultDuration(context.Context) time.Duration {
	return 90 * time.Minute
}
func (stage4SettingsStub) AppointmentBuffer(context.Context) time.Duration         { return 15 * time.Minute }
func (stage4SettingsStub) AppointmentDailySoftLimit(context.Context) int           { return 6 }
func (stage4SettingsStub) AppointmentWorkdayHours(context.Context) (int, int)      { return 9, 18 }
func (stage4SettingsStub) AppointmentNoShowFollowup(context.Context) time.Duration { return time.Hour }

func TestStage4CreateAppointmentUpdatesLeadAndRejectsDuplicate(t *testing.T) {
	uc, leads, _, _ := newStage4UseCase(t)
	lead := stage4Lead(101, "inviter-1")
	mustCreateStage4Lead(t, leads, lead)
	start := tomorrowAt(10, 0)
	appointment, err := uc.Create(context.Background(), stage4CreateRequest(lead.ID, start))
	if err != nil {
		t.Fatal(err)
	}
	if appointment.Status != types.AppointmentPendingAssignment || appointment.InviterUserID != "inviter-1" {
		t.Fatalf("appointment = %+v", appointment)
	}
	updated, _ := leads.GetByID(context.Background(), lead.ID)
	if updated.CurrentAppointmentID == nil || *updated.CurrentAppointmentID != appointment.ID || updated.OwnershipState != types.OwnershipAppointmentPending {
		t.Fatalf("lead = %+v", updated)
	}
	if _, err := uc.Create(context.Background(), stage4CreateRequest(lead.ID, start.Add(3*time.Hour))); err != types.ErrAppointmentAlreadyActive {
		t.Fatalf("duplicate error = %v", err)
	}
}

func TestStage4HardOverlapAndUnavailableCannotBeForced(t *testing.T) {
	uc, leads, appointments, availability := newStage4UseCase(t)
	start := tomorrowAt(10, 0)
	first := createStage4Appointment(t, uc, leads, 102, start)
	if _, err := uc.AssignCloser(context.Background(), &AssignCloserRequest{AppointmentID: first.ID, ActorUserID: "manager-1", ActorRole: "manager", CloserUserID: "closer-1", ExpectedVersion: 0}); err != nil {
		t.Fatal(err)
	}
	second := createStage4Appointment(t, uc, leads, 103, start.Add(30*time.Minute))
	_, err := uc.AssignCloser(context.Background(), &AssignCloserRequest{AppointmentID: second.ID, ActorUserID: "manager-1", ActorRole: "manager", CloserUserID: "closer-1", ExpectedVersion: 0, ForceSoftConflict: true, OverrideReason: "尝试强制"})
	if !errors.Is(err, types.ErrAppointmentHardConflict) {
		t.Fatalf("overlap error = %v", err)
	}

	block := &types.CloserAvailability{CloserUserID: "closer-2", StartAt: start, EndAt: start.Add(2 * time.Hour), Kind: types.AvailabilityKindUnavailable, Capacity: 1, CreatedBy: "manager-1", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := availability.Create(context.Background(), block); err != nil {
		t.Fatal(err)
	}
	third := createStage4Appointment(t, uc, leads, 104, start)
	_, err = uc.AssignCloser(context.Background(), &AssignCloserRequest{AppointmentID: third.ID, ActorUserID: "manager-1", ActorRole: "manager", CloserUserID: "closer-2", ExpectedVersion: 0, ForceSoftConflict: true, OverrideReason: "不可用也不能覆盖"})
	if !errors.Is(err, types.ErrAppointmentHardConflict) {
		t.Fatalf("unavailable error = %v", err)
	}
	stored, _ := appointments.GetByID(context.Background(), third.ID)
	if stored.Status != types.AppointmentPendingAssignment {
		t.Fatalf("hard conflict changed appointment: %+v", stored)
	}
}

func TestStage4SoftConflictRequiresReasonAndCanBeOverridden(t *testing.T) {
	uc, leads, _, _ := newStage4UseCase(t)
	appointment := createStage4Appointment(t, uc, leads, 105, tomorrowAt(20, 0))
	request := &AssignCloserRequest{AppointmentID: appointment.ID, ActorUserID: "manager-1", ActorRole: "manager", CloserUserID: "closer-1", ExpectedVersion: 0}
	conflicts, err := uc.AssignCloser(context.Background(), request)
	if !errors.Is(err, types.ErrAppointmentSoftConflict) || len(conflicts) == 0 {
		t.Fatalf("soft conflict = %+v err=%v", conflicts, err)
	}
	request.ForceSoftConflict = true
	if _, err := uc.AssignCloser(context.Background(), request); err != types.ErrConflictOverrideRequired {
		t.Fatalf("missing reason error = %v", err)
	}
	request.OverrideReason = "客户只能晚间到校，主管确认安排"
	if _, err := uc.AssignCloser(context.Background(), request); err != nil {
		t.Fatal(err)
	}
}

func TestStage4ConfiguredWorkingHoursWarnOutsideCustomShift(t *testing.T) {
	uc, leads, _, availability := newStage4UseCase(t)
	day := tomorrowAt(9, 0)
	block := &types.CloserAvailability{
		CloserUserID: "closer-1",
		StartAt:      day,
		EndAt:        day.Add(3 * time.Hour),
		Kind:         types.AvailabilityKindWorking,
		Capacity:     1,
		CreatedBy:    "manager-1",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := availability.Create(context.Background(), block); err != nil {
		t.Fatal(err)
	}
	appointment := createStage4Appointment(t, uc, leads, 109, tomorrowAt(16, 0))
	conflicts, err := uc.AssignCloser(context.Background(), &AssignCloserRequest{
		AppointmentID:   appointment.ID,
		ActorUserID:     "manager-1",
		ActorRole:       "manager",
		CloserUserID:    "closer-1",
		ExpectedVersion: 0,
	})
	if !errors.Is(err, types.ErrAppointmentSoftConflict) {
		t.Fatalf("outside custom shift error = %v", err)
	}
	found := false
	for _, conflict := range conflicts {
		if conflict.Code == types.ConflictCodeOutsideWorktime {
			found = true
		}
	}
	if !found {
		t.Fatalf("outside custom shift conflicts = %+v", conflicts)
	}
}

func TestStage4ConcurrentCloserAssignmentOnlyOneSucceeds(t *testing.T) {
	uc, leads, appointments, _ := newStage4UseCase(t)
	appointment := createStage4Appointment(t, uc, leads, 106, tomorrowAt(14, 0))
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, closer := range []string{"closer-1", "closer-2"} {
		wg.Add(1)
		go func(closer string) {
			defer wg.Done()
			_, err := uc.AssignCloser(context.Background(), &AssignCloserRequest{AppointmentID: appointment.ID, ActorUserID: "manager-1", ActorRole: "manager", CloserUserID: closer, ExpectedVersion: 0})
			errs <- err
		}(closer)
	}
	wg.Wait()
	close(errs)
	successes := 0
	for err := range errs {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("assignment successes = %d, want 1", successes)
	}
	stored, _ := appointments.GetByID(context.Background(), appointment.ID)
	if stored.Version != 1 || stored.Status != types.AppointmentScheduled {
		t.Fatalf("stored appointment = %+v", stored)
	}
}

func TestStage4CancelReleasesLeadAndNoShowAddsFollowupDeadline(t *testing.T) {
	uc, leads, appointments, _ := newStage4UseCase(t)
	appointment := createStage4Appointment(t, uc, leads, 107, tomorrowAt(11, 0))
	if err := uc.Cancel(context.Background(), &AppointmentActionRequest{AppointmentID: appointment.ID, ActorUserID: "inviter-1", ActorRole: "inviter", ExpectedVersion: 0, Reason: "客户取消"}); err != nil {
		t.Fatal(err)
	}
	lead, _ := leads.GetByID(context.Background(), appointment.LeadID)
	if lead.CurrentAppointmentID != nil || lead.OwnershipState != types.OwnershipPrivate {
		t.Fatalf("cancelled lead = %+v", lead)
	}
	stored, _ := appointments.GetByID(context.Background(), appointment.ID)
	if stored.Status != types.AppointmentCancelled {
		t.Fatalf("cancelled appointment = %+v", stored)
	}

	pastLead := stage4Lead(108, "inviter-1")
	mustCreateStage4Lead(t, leads, pastLead)
	now := time.Now()
	past := &types.LeadAppointment{AppointmentNo: "AP-PAST", LeadID: pastLead.ID, InviterUserID: "inviter-1", CloserUserID: "closer-1", StartAt: now.Add(-2 * time.Hour), EndAt: now.Add(-time.Hour), BusinessTimezone: "Asia/Shanghai", Campus: "襄阳校区", VisitorCount: 1, HandoffNotes: "测试", Status: types.AppointmentScheduled, CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now.Add(-24 * time.Hour)}
	if err := appointments.Create(context.Background(), past); err != nil {
		t.Fatal(err)
	}
	pastLead.CurrentAppointmentID = &past.ID
	pastLead.CloserUserID = "closer-1"
	pastLead.OwnershipState = types.OwnershipAppointmentScheduled
	pastLead.Version++
	pastLead.UpdatedAt = now
	if err := leads.Update(context.Background(), pastLead); err != nil {
		t.Fatal(err)
	}
	if err := uc.NoShow(context.Background(), &AppointmentActionRequest{AppointmentID: past.ID, ActorUserID: "closer-1", ActorRole: "closer", ExpectedVersion: 0, Reason: "客户未到"}); err != nil {
		t.Fatal(err)
	}
	noShowLead, _ := leads.GetByID(context.Background(), pastLead.ID)
	if noShowLead.CurrentAppointmentID != nil || noShowLead.NextFollowupDueAt == nil || noShowLead.CloserUserID != "" {
		t.Fatalf("no-show lead = %+v", noShowLead)
	}
}

func newStage4UseCase(t *testing.T) (*AppointmentUseCase, repo.LeadRepository, repo.AppointmentRepository, repo.CloserAvailabilityRepository) {
	t.Helper()
	db := newStage3TestDB(t)
	leads := repo.NewGormLeadRepository(db)
	appointments := repo.NewGormAppointmentRepository(db)
	availability := repo.NewGormCloserAvailabilityRepository(db)
	bindings := repo.NewGormUserBindingRepository(db)
	now := time.Now()
	for i, item := range []*types.WeComUserBinding{{WeComUserID: "inviter-1", EmployeeName: "邀约师", Role: types.UserRoleInviter, Status: types.UserStatusActive}, {WeComUserID: "closer-1", EmployeeName: "谈单师一", Role: types.UserRoleCloser, Status: types.UserStatusActive}, {WeComUserID: "closer-2", EmployeeName: "谈单师二", Role: types.UserRoleCloser, Status: types.UserStatusActive}} {
		item.CRMUserID = int64(i + 1)
		item.CreatedAt, item.UpdatedAt = now, now
		if err := bindings.Create(context.Background(), item); err != nil {
			t.Fatal(err)
		}
	}
	settings := stage4SettingsStub{}
	return NewAppointmentUseCase(appointments, availability, leads, bindings, repo.NewGormTransactionManager(db), settings, settings), leads, appointments, availability
}

func createStage4Appointment(t *testing.T, uc *AppointmentUseCase, leads repo.LeadRepository, id int64, start time.Time) *types.LeadAppointment {
	t.Helper()
	lead := stage4Lead(id, "inviter-1")
	mustCreateStage4Lead(t, leads, lead)
	appointment, err := uc.Create(context.Background(), stage4CreateRequest(lead.ID, start))
	if err != nil {
		t.Fatal(err)
	}
	return appointment
}

func stage4CreateRequest(leadID int64, start time.Time) *CreateAppointmentRequest {
	return &CreateAppointmentRequest{LeadID: leadID, ActorUserID: "inviter-1", ActorRole: "inviter", StartAt: start, EndAt: start.Add(90 * time.Minute), BusinessTimezone: "Asia/Shanghai", Campus: "襄阳校区", VisitorCount: 1, HandoffNotes: "客户关注课程和费用"}
}

func stage4Lead(id int64, inviter string) *types.Lead {
	lead := stage3OwnedLead(id, inviter)
	lead.LeadNo = "STAGE4-" + time.Now().Format("150405.000000")
	lead.InviterUserID = inviter
	return lead
}

func mustCreateStage4Lead(t *testing.T, leads repo.LeadRepository, lead *types.Lead) {
	t.Helper()
	if err := leads.Create(context.Background(), lead); err != nil {
		t.Fatal(err)
	}
}

func tomorrowAt(hour, minute int) time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day()+1, hour, minute, 0, 0, now.Location())
}
