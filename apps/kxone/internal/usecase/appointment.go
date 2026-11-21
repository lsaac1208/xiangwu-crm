package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/runtime"
	"github.com/lsaac128/kxone/pkg/types"
)

type AppointmentConflictError struct {
	Kind      error
	Conflicts []types.AppointmentConflict
}

func (e *AppointmentConflictError) Error() string { return e.Kind.Error() }
func (e *AppointmentConflictError) Unwrap() error { return e.Kind }

type AppointmentUseCase struct {
	appointments  repo.AppointmentRepository
	availability  repo.CloserAvailabilityRepository
	leads         repo.LeadRepository
	bindings      repo.UserBindingRepository
	txManager     repo.TransactionManager
	flags         runtime.WorkflowFeatureFlags
	settings      runtime.AppointmentPolicySettings
	followup      runtime.FollowupPolicySettings
	activities    repo.LeadActivityRepository
	reminders     repo.LeadReminderRepository
	notifications TaskQueue
}

func (uc *AppointmentUseCase) WithStage5Delivery(settings runtime.FollowupPolicySettings, activities repo.LeadActivityRepository, reminders repo.LeadReminderRepository) *AppointmentUseCase {
	uc.followup = settings
	uc.activities = activities
	uc.reminders = reminders
	return uc
}

func (uc *AppointmentUseCase) WithNotifications(queue TaskQueue) *AppointmentUseCase {
	uc.notifications = queue
	return uc
}

func NewAppointmentUseCase(appointments repo.AppointmentRepository, availability repo.CloserAvailabilityRepository, leads repo.LeadRepository, bindings repo.UserBindingRepository, txManager repo.TransactionManager, flags runtime.WorkflowFeatureFlags, settings runtime.AppointmentPolicySettings) *AppointmentUseCase {
	return &AppointmentUseCase{appointments: appointments, availability: availability, leads: leads, bindings: bindings, txManager: txManager, flags: flags, settings: settings}
}

type CreateAppointmentRequest struct {
	LeadID           int64
	ActorUserID      string
	ActorRole        string
	StartAt          time.Time
	EndAt            time.Time
	BusinessTimezone string
	Campus           string
	VisitorCount     int
	CustomerConcerns string
	HandoffNotes     string
	SpecialNotes     string
}

type AssignCloserRequest struct {
	AppointmentID     int64
	ActorUserID       string
	ActorRole         string
	CloserUserID      string
	ExpectedVersion   int
	ForceSoftConflict bool
	OverrideReason    string
}

type RescheduleAppointmentRequest struct {
	AppointmentID     int64
	ActorUserID       string
	ActorRole         string
	StartAt           time.Time
	EndAt             time.Time
	ExpectedVersion   int
	ForceSoftConflict bool
	OverrideReason    string
	Reason            string
}

type AppointmentActionRequest struct {
	AppointmentID   int64
	ActorUserID     string
	ActorRole       string
	ExpectedVersion int
	Reason          string
}

type ConfirmArrivalRequest struct {
	AppointmentID   int64
	ActorUserID     string
	ActorRole       string
	ExpectedVersion int
	Confirmation    string
}

type ListAppointmentsRequest struct {
	ActorUserID  string
	ActorRole    string
	LeadID       int64
	CloserUserID string
	Status       string
	StartAt      time.Time
	EndAt        time.Time
}

type AppointmentView struct {
	*types.LeadAppointment
	LeadNo                string `json:"lead_no,omitempty"`
	CustomerName          string `json:"customer_name,omitempty"`
	InviterName           string `json:"inviter_name,omitempty"`
	CloserName            string `json:"closer_name,omitempty"`
	HandoffPending        bool   `json:"handoff_pending"`
	PendingInviterUserID  string `json:"pending_inviter_userid,omitempty"`
	PendingInviterName    string `json:"pending_inviter_name,omitempty"`
	OriginalCreatorUserID string `json:"original_creator_userid,omitempty"`
	OriginalCreatorName   string `json:"original_creator_name"`
}

func (uc *AppointmentUseCase) enabled(ctx context.Context) bool {
	return uc.flags != nil && uc.flags.WorkflowV2Enabled(ctx) && uc.flags.AppointmentEnabled(ctx)
}

func (uc *AppointmentUseCase) Create(ctx context.Context, req *CreateAppointmentRequest) (*types.LeadAppointment, error) {
	if !uc.enabled(ctx) {
		return nil, types.ErrAppointmentForbidden
	}
	if err := validateAppointmentTimes(req.StartAt, req.EndAt, time.Now()); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Campus) == "" || strings.TrimSpace(req.HandoffNotes) == "" {
		return nil, types.ErrAppointmentDetailsRequired
	}
	var created *types.LeadAppointment
	var managerUserID string
	err := uc.withTransaction(ctx, func(ctx context.Context, appointments repo.AppointmentRepository, _ repo.CloserAvailabilityRepository, leads repo.LeadRepository, _ repo.UserBindingRepository, reminders repo.LeadReminderRepository) error {
		lead, err := leads.GetByID(ctx, req.LeadID)
		if err != nil || lead == nil {
			if err != nil {
				return err
			}
			return types.ErrLeadNotFound
		}
		if lead.BusinessStage != types.BusinessStageInvitation || lead.ResultStatus != types.LeadResultActive || lead.OwnershipState == types.OwnershipPublicPool || lead.OwnershipState == types.OwnershipAssignedPendingClaim || lead.InviterUserID == "" {
			return types.ErrAppointmentForbidden
		}
		if !canCreateAppointment(lead, req.ActorUserID, req.ActorRole) {
			return types.ErrUnauthorized
		}
		active, err := appointments.GetActiveByLead(ctx, lead.ID)
		if err != nil {
			return err
		}
		if active != nil {
			return types.ErrAppointmentAlreadyActive
		}
		now := time.Now()
		visitorCount := req.VisitorCount
		if visitorCount < 1 {
			visitorCount = 1
		}
		appointment := &types.LeadAppointment{
			AppointmentNo: fmt.Sprintf("AP-%s-%d", now.Format("20060102150405"), now.UnixNano()%1000000),
			LeadID:        lead.ID, InviterUserID: lead.InviterUserID, StartAt: req.StartAt.UTC(), EndAt: req.EndAt.UTC(),
			BusinessTimezone: "Asia/Shanghai", Campus: strings.TrimSpace(req.Campus), VisitorCount: visitorCount,
			CustomerConcerns: strings.TrimSpace(req.CustomerConcerns), HandoffNotes: strings.TrimSpace(req.HandoffNotes), SpecialNotes: strings.TrimSpace(req.SpecialNotes),
			Status: types.AppointmentPendingAssignment, CreatedAt: now, UpdatedAt: now,
		}
		if err := appointments.Create(ctx, appointment); err != nil {
			return err
		}
		if err := createAppointmentEvent(ctx, appointments, appointment, req.ActorUserID, req.ActorRole, types.AppointmentEventCreated, "", string(appointment.Status), ""); err != nil {
			return err
		}
		lead.CurrentAppointmentID = &appointment.ID
		lead.OwnershipState = types.OwnershipAppointmentPending
		lead.Version++
		lead.UpdatedAt = now
		if err := leads.Update(ctx, lead); err != nil {
			return err
		}
		created = appointment
		managerUserID = lead.DispatchManagerUserID
		return nil
	})
	if err == nil && created != nil {
		uc.enqueueAppointmentCards(ctx, created.LeadID, created.ID, created.Version, types.CardSceneAppointmentPendingAssignment, managerUserID)
	}
	return created, err
}

func (uc *AppointmentUseCase) AssignCloser(ctx context.Context, req *AssignCloserRequest) ([]types.AppointmentConflict, error) {
	if !uc.enabled(ctx) {
		return nil, types.ErrAppointmentForbidden
	}
	if req.ActorRole != string(types.UserRoleManager) && req.ActorRole != string(types.UserRoleAdmin) {
		return nil, types.ErrUnauthorized
	}
	if err := validateAssignableCloser(ctx, uc.bindings, req.CloserUserID); err != nil {
		return nil, err
	}
	appointmentSettings := snapshotAppointmentPolicy(ctx, uc.settings)
	var conflicts []types.AppointmentConflict
	var changed *types.LeadAppointment
	err := uc.withTransaction(ctx, func(ctx context.Context, appointments repo.AppointmentRepository, availability repo.CloserAvailabilityRepository, leads repo.LeadRepository, bindings repo.UserBindingRepository, reminders repo.LeadReminderRepository) error {
		if err := validateAssignableCloser(ctx, bindings, req.CloserUserID); err != nil {
			return err
		}
		appointment, err := appointments.GetByID(ctx, req.AppointmentID)
		if err != nil || appointment == nil {
			if err != nil {
				return err
			}
			return types.ErrAppointmentNotFound
		}
		if appointment.Status != types.AppointmentPendingAssignment && appointment.Status != types.AppointmentScheduled {
			return types.ErrAppointmentStatusInvalid
		}
		if appointment.Version != req.ExpectedVersion {
			return types.ErrAppointmentVersionConflict
		}
		conflicts, err = uc.detectConflicts(ctx, appointmentSettings, appointments, availability, req.CloserUserID, appointment.StartAt, appointment.EndAt, appointment.Campus, appointment.ID)
		if err != nil {
			return err
		}
		if err := validateConflictOverride(conflicts, req.ForceSoftConflict, req.OverrideReason); err != nil {
			return err
		}
		oldCloser, oldStatus := appointment.CloserUserID, appointment.Status
		appointment.CloserUserID = req.CloserUserID
		appointment.AssignedByUserID = req.ActorUserID
		appointment.Status = types.AppointmentScheduled
		appointment.UpdatedAt = time.Now()
		updated, err := appointments.UpdateCAS(ctx, appointment, req.ExpectedVersion)
		if err != nil {
			return err
		}
		if !updated {
			return types.ErrAppointmentVersionConflict
		}
		eventType := types.AppointmentEventCloserAssigned
		if oldCloser != "" && oldCloser != req.CloserUserID {
			eventType = types.AppointmentEventCloserChanged
		}
		if err := createAppointmentEvent(ctx, appointments, appointment, req.ActorUserID, req.ActorRole, eventType, string(oldStatus), string(appointment.Status), strings.TrimSpace(req.OverrideReason)); err != nil {
			return err
		}
		lead, err := leads.GetByID(ctx, appointment.LeadID)
		if err != nil || lead == nil {
			if err != nil {
				return err
			}
			return types.ErrLeadNotFound
		}
		if lead.ResultStatus != types.LeadResultActive || lead.CurrentAppointmentID == nil || *lead.CurrentAppointmentID != appointment.ID {
			return types.ErrAppointmentStatusInvalid
		}
		lead.CloserUserID = req.CloserUserID
		lead.CurrentAppointmentID = &appointment.ID
		if !appointmentHandoffPending(lead) {
			lead.OwnershipState = types.OwnershipAppointmentScheduled
		}
		lead.Version++
		lead.UpdatedAt = time.Now()
		if err := leads.Update(ctx, lead); err != nil {
			return err
		}
		changed = appointment
		return writeAppointmentReminders(ctx, reminders, appointment, lead)
	})
	if err == nil && changed != nil {
		uc.enqueueAppointmentCards(ctx, changed.LeadID, changed.ID, changed.Version, types.CardSceneAppointmentAssigned, changed.InviterUserID, changed.CloserUserID)
	}
	return conflicts, err
}

func (uc *AppointmentUseCase) Reschedule(ctx context.Context, req *RescheduleAppointmentRequest) ([]types.AppointmentConflict, error) {
	if !uc.enabled(ctx) {
		return nil, types.ErrAppointmentForbidden
	}
	if err := validateAppointmentTimes(req.StartAt, req.EndAt, time.Now()); err != nil {
		return nil, err
	}
	appointmentSettings := snapshotAppointmentPolicy(ctx, uc.settings)
	var conflicts []types.AppointmentConflict
	var changed *types.LeadAppointment
	err := uc.withTransaction(ctx, func(ctx context.Context, appointments repo.AppointmentRepository, availability repo.CloserAvailabilityRepository, leads repo.LeadRepository, _ repo.UserBindingRepository, reminders repo.LeadReminderRepository) error {
		appointment, err := appointments.GetByID(ctx, req.AppointmentID)
		if err != nil || appointment == nil {
			if err != nil {
				return err
			}
			return types.ErrAppointmentNotFound
		}
		if appointment.Status != types.AppointmentPendingAssignment && appointment.Status != types.AppointmentScheduled {
			return types.ErrAppointmentStatusInvalid
		}
		if appointment.Version != req.ExpectedVersion {
			return types.ErrAppointmentVersionConflict
		}
		lead, err := leads.GetByID(ctx, appointment.LeadID)
		if err != nil {
			return err
		}
		if lead == nil {
			return types.ErrLeadNotFound
		}
		if lead.ResultStatus != types.LeadResultActive || lead.CurrentAppointmentID == nil || *lead.CurrentAppointmentID != appointment.ID {
			return types.ErrAppointmentStatusInvalid
		}
		if !canModifyLeadAppointment(appointment, lead, req.ActorUserID, req.ActorRole) {
			return types.ErrUnauthorized
		}
		if appointment.Status == types.AppointmentScheduled && req.ActorRole != string(types.UserRoleManager) && req.ActorRole != string(types.UserRoleAdmin) {
			return types.ErrUnauthorized
		}
		if appointment.CloserUserID != "" {
			conflicts, err = uc.detectConflicts(ctx, appointmentSettings, appointments, availability, appointment.CloserUserID, req.StartAt, req.EndAt, appointment.Campus, appointment.ID)
			if err != nil {
				return err
			}
			if err := validateConflictOverride(conflicts, req.ForceSoftConflict, req.OverrideReason); err != nil {
				return err
			}
		}
		from := appointment.Status
		appointment.StartAt, appointment.EndAt = req.StartAt.UTC(), req.EndAt.UTC()
		appointment.RescheduledReason = strings.TrimSpace(req.Reason)
		appointment.UpdatedAt = time.Now()
		updated, err := appointments.UpdateCAS(ctx, appointment, req.ExpectedVersion)
		if err != nil {
			return err
		}
		if !updated {
			return types.ErrAppointmentVersionConflict
		}
		if err := createAppointmentEvent(ctx, appointments, appointment, req.ActorUserID, req.ActorRole, types.AppointmentEventRescheduled, string(from), string(appointment.Status), strings.TrimSpace(req.Reason+" "+req.OverrideReason)); err != nil {
			return err
		}
		changed = appointment
		return writeAppointmentReminders(ctx, reminders, appointment, lead)
	})
	if err == nil && changed != nil {
		uc.enqueueAppointmentCards(ctx, changed.LeadID, changed.ID, changed.Version, types.CardSceneAppointmentRescheduled, changed.InviterUserID, changed.CloserUserID)
	}
	return conflicts, err
}

func writeAppointmentReminders(ctx context.Context, reminders repo.LeadReminderRepository, appointment *types.LeadAppointment, lead *types.Lead) error {
	if reminders == nil || appointment == nil || appointment.Status != types.AppointmentScheduled || appointmentHandoffPending(lead) {
		return nil
	}
	receivers := []string{appointment.InviterUserID, appointment.CloserUserID}
	managerUserID := ""
	if lead != nil {
		managerUserID = lead.DispatchManagerUserID
	}
	definitions := []struct {
		reminderType string
		dueAt        time.Time
	}{
		{types.ReminderTypeAppointmentUpcoming, appointment.StartAt.Add(-30 * time.Minute)},
		{types.ReminderTypeAppointmentUnconfirmed, appointment.StartAt},
	}
	now := time.Now()
	for _, definition := range definitions {
		definitionReceivers := receivers
		if definition.reminderType == types.ReminderTypeAppointmentUnconfirmed && managerUserID != "" {
			definitionReceivers = append(append([]string{}, receivers...), managerUserID)
		}
		seen := map[string]struct{}{}
		for _, receiver := range definitionReceivers {
			receiver = strings.TrimSpace(receiver)
			if receiver == "" {
				continue
			}
			if _, exists := seen[receiver]; exists {
				continue
			}
			seen[receiver] = struct{}{}
			key := fmt.Sprintf("appointment:%d:v%d:%s:%s", appointment.ID, appointment.Version, definition.reminderType, receiver)
			appointmentID := appointment.ID
			if _, err := reminders.Create(ctx, &types.LeadReminderRecord{
				LeadID: appointment.LeadID, AppointmentID: &appointmentID, ReminderType: definition.reminderType,
				BusinessVersion: appointment.Version, DueAt: definition.dueAt, ReceiverUserID: receiver,
				Status: types.ReminderStatusPending, IdempotencyKey: key, CreatedAt: now, UpdatedAt: now,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (uc *AppointmentUseCase) Cancel(ctx context.Context, req *AppointmentActionRequest) error {
	return uc.closeAppointment(ctx, req, types.AppointmentCancelled)
}

func (uc *AppointmentUseCase) NoShow(ctx context.Context, req *AppointmentActionRequest) error {
	return uc.closeAppointment(ctx, req, types.AppointmentNoShow)
}

func (uc *AppointmentUseCase) ConfirmArrival(ctx context.Context, req *ConfirmArrivalRequest) error {
	if !uc.enabled(ctx) {
		return types.ErrAppointmentForbidden
	}
	if req == nil {
		return types.ErrDeliveryForbidden
	}
	followupSettings := snapshotFollowupPolicy(ctx, uc.followup)
	var changed *types.LeadAppointment
	var managerUserID string
	err := uc.withDeliveryTransaction(ctx, func(ctx context.Context, appointments repo.AppointmentRepository, leads repo.LeadRepository, activities repo.LeadActivityRepository, reminders repo.LeadReminderRepository) error {
		appointment, err := appointments.GetByID(ctx, req.AppointmentID)
		if err != nil || appointment == nil {
			if err != nil {
				return err
			}
			return types.ErrAppointmentNotFound
		}
		if appointment.Status == types.AppointmentArrivedDelivered {
			return types.ErrDeliveryAlreadyCompleted
		}
		if appointment.Status != types.AppointmentScheduled || appointment.CloserUserID == "" {
			return types.ErrDeliveryForbidden
		}
		if appointment.Version != req.ExpectedVersion {
			return types.ErrAppointmentVersionConflict
		}
		if req.ActorRole != string(types.UserRoleAdmin) && req.ActorRole != string(types.UserRoleManager) &&
			!(types.IsInvitationRole(types.UserRole(req.ActorRole)) && appointment.InviterUserID == req.ActorUserID) {
			return types.ErrUnauthorized
		}

		lead, err := leads.GetByID(ctx, appointment.LeadID)
		if err != nil || lead == nil {
			if err != nil {
				return err
			}
			return types.ErrLeadNotFound
		}
		types.ProjectLegacyWorkflow(lead)
		if lead.BusinessStage != types.BusinessStageInvitation || lead.ResultStatus != types.LeadResultActive ||
			lead.CurrentAppointmentID == nil || *lead.CurrentAppointmentID != appointment.ID || lead.InviterUserID != appointment.InviterUserID || appointmentHandoffPending(lead) {
			return types.ErrDeliveryForbidden
		}

		now := time.Now()
		fromStatus := appointment.Status
		appointment.Status = types.AppointmentArrivedDelivered
		appointment.ArrivedAt = &now
		appointment.UpdatedAt = now
		updated, err := appointments.UpdateCAS(ctx, appointment, req.ExpectedVersion)
		if err != nil {
			return err
		}
		if !updated {
			return types.ErrAppointmentVersionConflict
		}
		if err := createAppointmentEvent(ctx, appointments, appointment, req.ActorUserID, req.ActorRole, types.AppointmentEventArrivedDelivered, string(fromStatus), string(appointment.Status), strings.TrimSpace(req.Confirmation)); err != nil {
			return err
		}

		before, _ := json.Marshal(map[string]any{"business_stage": lead.BusinessStage, "owner_userid": lead.CurrentOwnerUserID, "owner_role": lead.CurrentOwnerRole})
		lead.CurrentAppointmentID = nil // Arrived appointments remain immutable history, not current reservations.
		lead.BusinessStage = types.BusinessStageNegotiation
		lead.OwnershipState = types.OwnershipPrivate
		lead.ResultStatus = types.LeadResultActive
		lead.Status = types.LeadStatusFollowing
		lead.CloserUserID = appointment.CloserUserID
		lead.CurrentOwnerUserID = appointment.CloserUserID
		lead.CurrentOwnerRole = types.OwnerRoleCloser
		lead.AssignedSalesUserID = appointment.CloserUserID
		lead.OwnerUserID = appointment.CloserUserID
		types.BeginOwnerCycle(lead, now)
		lead.ArrivedAt = &now
		lead.DeliveredAt = &now
		if followupSettings != nil {
			lead.NextFollowupDueAt = timePointer(now.Add(followupSettings.FirstFollowupDue(ctx)))
		} else {
			lead.NextFollowupDueAt = nil
		}
		lead.Version++
		lead.UpdatedAt = now
		if err := leads.Update(ctx, lead); err != nil {
			return err
		}
		after, _ := json.Marshal(map[string]any{"business_stage": lead.BusinessStage, "owner_userid": lead.CurrentOwnerUserID, "owner_role": lead.CurrentOwnerRole})
		metadata, _ := json.Marshal(map[string]any{"appointment_id": appointment.ID, "inviter_userid": appointment.InviterUserID, "closer_userid": appointment.CloserUserID, "confirmation": strings.TrimSpace(req.Confirmation)})
		if activities != nil {
			if err := activities.Create(ctx, &types.LeadActivityRecord{LeadID: lead.ID, ActivityType: types.LeadActivityArrivedDelivered, ActorUserID: req.ActorUserID, ActorRole: req.ActorRole, FromValueJSON: string(before), ToValueJSON: string(after), MetadataJSON: string(metadata), CreatedAt: now}); err != nil {
				return err
			}
		}
		if reminders != nil && followupSettings != nil {
			if err := scheduleFollowupReminders(ctx, reminders, followupSettings, lead); err != nil {
				return err
			}
		}
		changed = appointment
		managerUserID = lead.DispatchManagerUserID
		return nil
	})
	if err == nil && changed != nil {
		uc.enqueueAppointmentCards(ctx, changed.LeadID, changed.ID, changed.Version, types.CardSceneAppointmentArrivedDelivered, changed.InviterUserID, changed.CloserUserID, managerUserID)
	}
	return err
}

func (uc *AppointmentUseCase) closeAppointment(ctx context.Context, req *AppointmentActionRequest, target types.AppointmentStatus) error {
	if !uc.enabled(ctx) {
		return types.ErrAppointmentForbidden
	}
	if strings.TrimSpace(req.Reason) == "" {
		return types.ErrInvalidReasonRequired
	}
	appointmentSettings := snapshotAppointmentPolicy(ctx, uc.settings)
	followupSettings := snapshotFollowupPolicy(ctx, uc.followup)
	var changed *types.LeadAppointment
	var managerUserID string
	err := uc.withTransaction(ctx, func(ctx context.Context, appointments repo.AppointmentRepository, _ repo.CloserAvailabilityRepository, leads repo.LeadRepository, _ repo.UserBindingRepository, reminders repo.LeadReminderRepository) error {
		appointment, err := appointments.GetByID(ctx, req.AppointmentID)
		if err != nil || appointment == nil {
			if err != nil {
				return err
			}
			return types.ErrAppointmentNotFound
		}
		if appointment.Status != types.AppointmentPendingAssignment && appointment.Status != types.AppointmentScheduled {
			return types.ErrAppointmentStatusInvalid
		}
		if appointment.Version != req.ExpectedVersion {
			return types.ErrAppointmentVersionConflict
		}
		lead, err := leads.GetByID(ctx, appointment.LeadID)
		if err != nil {
			return err
		}
		if lead == nil {
			return types.ErrLeadNotFound
		}
		pendingHandoff := appointmentHandoffPending(lead)
		if !canModifyLeadAppointment(appointment, lead, req.ActorUserID, req.ActorRole) && !(target == types.AppointmentNoShow && !pendingHandoff && req.ActorRole == string(types.UserRoleCloser) && req.ActorUserID == appointment.CloserUserID) {
			return types.ErrUnauthorized
		}
		if target == types.AppointmentNoShow && time.Now().Before(appointment.StartAt) {
			return types.ErrAppointmentStatusInvalid
		}
		from := appointment.Status
		appointment.Status = target
		if target == types.AppointmentCancelled {
			appointment.CancelledReason = strings.TrimSpace(req.Reason)
		} else {
			appointment.NoShowReason = strings.TrimSpace(req.Reason)
		}
		appointment.UpdatedAt = time.Now()
		updated, err := appointments.UpdateCAS(ctx, appointment, req.ExpectedVersion)
		if err != nil {
			return err
		}
		if !updated {
			return types.ErrAppointmentVersionConflict
		}
		eventType := types.AppointmentEventCancelled
		if target == types.AppointmentNoShow {
			eventType = types.AppointmentEventNoShow
		}
		if err := createAppointmentEvent(ctx, appointments, appointment, req.ActorUserID, req.ActorRole, eventType, string(from), string(target), strings.TrimSpace(req.Reason)); err != nil {
			return err
		}

		lead.CurrentAppointmentID = nil
		lead.CloserUserID = ""
		if !pendingHandoff {
			lead.OwnershipState = types.OwnershipPrivate
		}
		if !pendingHandoff && lead.ResultStatus == types.LeadResultActive && lead.NextFollowupDueAt == nil && followupSettings != nil && lead.CurrentOwnerStartedAt != nil {
			lead.NextFollowupDueAt = timePointer(lead.CurrentOwnerStartedAt.Add(followupSettings.FirstFollowupDue(ctx)))
		}
		if target == types.AppointmentNoShow && !pendingHandoff {
			due := time.Now().Add(appointmentSettings.AppointmentNoShowFollowup(ctx))
			lead.NextFollowupDueAt = &due
		}
		lead.Version++
		lead.UpdatedAt = time.Now()
		if err := leads.Update(ctx, lead); err != nil {
			return err
		}
		if err := scheduleFollowupReminders(ctx, reminders, followupSettings, lead); err != nil {
			return err
		}
		changed = appointment
		managerUserID = lead.DispatchManagerUserID
		return nil
	})
	if err == nil && changed != nil {
		scene := types.CardSceneAppointmentCancelled
		if target == types.AppointmentNoShow {
			scene = types.CardSceneAppointmentNoShow
		}
		uc.enqueueAppointmentCards(ctx, changed.LeadID, changed.ID, changed.Version, scene, changed.InviterUserID, changed.CloserUserID, managerUserID)
	}
	return err
}

func (uc *AppointmentUseCase) enqueueAppointmentCards(ctx context.Context, leadID, appointmentID int64, appointmentVersion int, scene types.CardScene, receiverUserIDs ...string) {
	if uc.notifications == nil {
		return
	}
	seen := make(map[string]struct{}, len(receiverUserIDs))
	for _, receiverUserID := range receiverUserIDs {
		receiverUserID = strings.TrimSpace(receiverUserID)
		if receiverUserID == "" {
			continue
		}
		if _, exists := seen[receiverUserID]; exists {
			continue
		}
		seen[receiverUserID] = struct{}{}
		var err error
		if eventQueue, ok := uc.notifications.(EventTaskQueue); ok {
			err = eventQueue.EnqueueSendCardEvent(ctx, leadID, receiverUserID, string(scene), fmt.Sprintf("appointment-%d-v%d", appointmentID, appointmentVersion))
		} else {
			err = uc.notifications.EnqueueSendCard(ctx, leadID, receiverUserID, string(scene))
		}
		if err != nil {
			slog.Error("failed to enqueue appointment notification", "lead_id", leadID, "receiver_userid", receiverUserID, "card_scene", scene, "err", err)
		}
	}
}

func (uc *AppointmentUseCase) List(ctx context.Context, req *ListAppointmentsRequest) ([]*AppointmentView, error) {
	if !uc.enabled(ctx) {
		return nil, types.ErrAppointmentForbidden
	}
	role := types.UserRole(req.ActorRole)
	filter := repo.AppointmentListFilter{LeadID: req.LeadID, CloserUserID: req.CloserUserID, Status: req.Status}
	if !req.StartAt.IsZero() {
		filter.EndAfter = &req.StartAt
	}
	if !req.EndAt.IsZero() {
		filter.StartBefore = &req.EndAt
	}

	if role == types.UserRoleCloser {
		filter.CloserUserID = req.ActorUserID
	}
	if role != types.UserRoleAdmin && role != types.UserRoleManager && !types.IsInvitationRole(role) && role != types.UserRoleCloser {
		return nil, types.ErrUnauthorized
	}
	items, err := uc.appointments.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	views := make([]*AppointmentView, 0, len(items))
	for _, item := range items {
		lead, err := uc.leads.GetByID(ctx, item.LeadID)
		if err != nil {
			return nil, err
		}
		if !canViewLeadAppointment(item, lead, req.ActorUserID, req.ActorRole) {
			continue
		}
		view := &AppointmentView{LeadAppointment: item, OriginalCreatorName: "未知"}
		if appointmentHandoffPending(lead) && *lead.CurrentAppointmentID == item.ID {
			view.HandoffPending = true
			view.PendingInviterUserID = lead.AssignedInviterUserID
			view.PendingInviterName = uc.bindingName(ctx, lead.AssignedInviterUserID)
		}
		events, eventErr := uc.appointments.ListEvents(ctx, item.ID)
		if eventErr != nil {
			return nil, eventErr
		}
		for _, event := range events {
			if event.EventType == types.AppointmentEventCreated {
				view.OriginalCreatorUserID = event.ActorUserID
				view.OriginalCreatorName = valueOr(uc.bindingName(ctx, event.ActorUserID), valueOr(event.ActorUserID, "未知"))
				break
			}
		}
		if lead != nil {
			view.LeadNo, view.CustomerName = lead.LeadNo, lead.CustomerName
		}
		view.InviterName = uc.bindingName(ctx, item.InviterUserID)
		view.CloserName = uc.bindingName(ctx, item.CloserUserID)
		views = append(views, view)
	}
	return views, nil
}

func (uc *AppointmentUseCase) Get(ctx context.Context, appointmentID int64, actorUserID, actorRole string) (*AppointmentView, []*types.LeadAppointmentEvent, error) {
	if !uc.enabled(ctx) {
		return nil, nil, types.ErrAppointmentForbidden
	}
	appointment, err := uc.appointments.GetByID(ctx, appointmentID)
	if err != nil || appointment == nil {
		if err != nil {
			return nil, nil, err
		}
		return nil, nil, types.ErrAppointmentNotFound
	}
	lead, err := uc.leads.GetByID(ctx, appointment.LeadID)
	if err != nil {
		return nil, nil, err
	}
	if !canViewLeadAppointment(appointment, lead, actorUserID, actorRole) {
		return nil, nil, types.ErrUnauthorized
	}
	items, err := uc.List(ctx, &ListAppointmentsRequest{ActorUserID: actorUserID, ActorRole: actorRole, LeadID: appointment.LeadID})
	if err != nil {
		return nil, nil, err
	}
	var view *AppointmentView
	for _, item := range items {
		if item.ID == appointmentID {
			view = item
			break
		}
	}
	if view == nil {
		view = &AppointmentView{LeadAppointment: appointment}
	}
	events, err := uc.appointments.ListEvents(ctx, appointmentID)
	return view, events, err
}

func (uc *AppointmentUseCase) DetectConflicts(ctx context.Context, closerUserID string, startAt, endAt time.Time, campus string, excludeID int64) ([]types.AppointmentConflict, error) {
	if !uc.enabled(ctx) {
		return nil, types.ErrAppointmentForbidden
	}
	if err := validateAssignableCloser(ctx, uc.bindings, closerUserID); err != nil {
		return nil, err
	}
	return uc.detectConflicts(ctx, snapshotAppointmentPolicy(ctx, uc.settings), uc.appointments, uc.availability, closerUserID, startAt, endAt, campus, excludeID)
}

func (uc *AppointmentUseCase) detectConflicts(ctx context.Context, settings runtime.AppointmentPolicySettings, appointments repo.AppointmentRepository, availability repo.CloserAvailabilityRepository, closer string, startAt, endAt time.Time, campus string, excludeID int64) ([]types.AppointmentConflict, error) {
	buffer := settings.AppointmentBuffer(ctx)
	nearby, err := appointments.ListCloserOverlaps(ctx, closer, startAt.Add(-buffer), endAt.Add(buffer), excludeID)
	if err != nil {
		return nil, err
	}
	dayStart := types.BusinessDayStart(startAt)
	dayEnd := dayStart.AddDate(0, 0, 1)
	blocks, err := availability.List(ctx, closer, dayStart, dayEnd)
	if err != nil {
		return nil, err
	}
	capacity := 1
	hasWorking, insideWorking := false, false
	conflicts := make([]types.AppointmentConflict, 0)
	for _, block := range blocks {
		overlaps := block.StartAt.Before(endAt) && block.EndAt.After(startAt)
		if block.Kind == types.AvailabilityKindUnavailable && overlaps {
			conflicts = appendConflict(conflicts, types.AppointmentConflict{Level: types.ConflictLevelHard, Code: types.ConflictCodeUnavailable, Message: "谈单师该时段不可用"})
		}
		if block.Kind == types.AvailabilityKindWorking {
			hasWorking = true
			if !startAt.Before(block.StartAt) && !endAt.After(block.EndAt) {
				insideWorking = true
				if block.Capacity > capacity {
					capacity = block.Capacity
				}
			}
		}
	}
	overlapCount := 0
	for _, item := range nearby {
		actual := item.StartAt.Before(endAt) && item.EndAt.After(startAt)
		if actual {
			overlapCount++
		} else {
			conflicts = appendConflict(conflicts, types.AppointmentConflict{Level: types.ConflictLevelSoft, Code: types.ConflictCodeBuffer, Message: "与相邻预约的缓冲时间不足", AppointmentID: item.ID})
		}
		if strings.TrimSpace(item.Campus) != "" && strings.TrimSpace(campus) != "" && item.Campus != campus {
			conflicts = appendConflict(conflicts, types.AppointmentConflict{Level: types.ConflictLevelSoft, Code: types.ConflictCodeCrossCampus, Message: "相邻预约跨校区", AppointmentID: item.ID})
		}
	}
	if overlapCount >= capacity {
		code, message := types.ConflictCodeCapacity, "该时段已超过谈单师并发容量"
		if capacity == 1 {
			code, message = types.ConflictCodeOverlap, "该时段已有预约"
		}
		conflicts = appendConflict(conflicts, types.AppointmentConflict{Level: types.ConflictLevelHard, Code: code, Message: message})
	}
	startHour, endHour := settings.AppointmentWorkdayHours(ctx)
	if (hasWorking && !insideWorking) || (!hasWorking && (startAt.Before(dayStart.Add(time.Duration(startHour)*time.Hour)) || endAt.After(dayStart.Add(time.Duration(endHour)*time.Hour)))) {
		conflicts = appendConflict(conflicts, types.AppointmentConflict{Level: types.ConflictLevelSoft, Code: types.ConflictCodeOutsideWorktime, Message: "预约超出谈单师工作时间"})
	}
	daily, err := appointments.List(ctx, repo.AppointmentListFilter{CloserUserID: closer, Status: string(types.AppointmentScheduled), EndAfter: &dayStart, StartBefore: &dayEnd})
	if err != nil {
		return nil, err
	}
	activeCount := 0
	for _, item := range daily {
		if item.ID != excludeID {
			activeCount++
		}
	}
	if activeCount >= settings.AppointmentDailySoftLimit(ctx) {
		conflicts = appendConflict(conflicts, types.AppointmentConflict{Level: types.ConflictLevelSoft, Code: types.ConflictCodeDailyOverload, Message: "谈单师当日预约量已达到提醒阈值"})
	}
	return conflicts, nil
}

func validateConflictOverride(conflicts []types.AppointmentConflict, force bool, reason string) error {
	var hard, soft []types.AppointmentConflict
	for _, conflict := range conflicts {
		if conflict.Level == types.ConflictLevelHard {
			hard = append(hard, conflict)
		} else {
			soft = append(soft, conflict)
		}
	}
	if len(hard) > 0 {
		return &AppointmentConflictError{Kind: types.ErrAppointmentHardConflict, Conflicts: hard}
	}
	if len(soft) > 0 && !force {
		return &AppointmentConflictError{Kind: types.ErrAppointmentSoftConflict, Conflicts: soft}
	}
	if len(soft) > 0 && strings.TrimSpace(reason) == "" {
		return types.ErrConflictOverrideRequired
	}
	return nil
}

func validateAppointmentTimes(startAt, endAt, now time.Time) error {
	if startAt.IsZero() || endAt.IsZero() || !endAt.After(startAt) || !startAt.After(now) || endAt.Sub(startAt) > 12*time.Hour {
		return types.ErrAppointmentTimeInvalid
	}
	return nil
}

func validateAssignableCloser(ctx context.Context, repository repo.UserBindingRepository, userID string) error {
	if repository == nil || strings.TrimSpace(userID) == "" {
		return types.ErrInvalidCloser
	}
	binding, err := repository.GetByWeComUserID(ctx, userID)
	if err != nil {
		return err
	}
	if binding == nil || binding.Status != types.UserStatusActive || binding.Role != types.UserRoleCloser {
		return types.ErrInvalidCloser
	}
	return nil
}

func canCreateAppointment(lead *types.Lead, actor, role string) bool {
	return role == string(types.UserRoleAdmin) || role == string(types.UserRoleManager) || (types.IsInvitationRole(types.UserRole(role)) && lead.InviterUserID == actor)
}
func canModifyAppointment(a *types.LeadAppointment, actor, role string) bool {
	return role == string(types.UserRoleAdmin) || role == string(types.UserRoleManager) || (types.IsInvitationRole(types.UserRole(role)) && a.InviterUserID == actor)
}
func canViewAppointment(a *types.LeadAppointment, actor, role string) bool {
	return canModifyAppointment(a, actor, role) || (role == string(types.UserRoleCloser) && a.CloserUserID == actor)
}

func createAppointmentEvent(ctx context.Context, repository repo.AppointmentRepository, appointment *types.LeadAppointment, actor, role, eventType, from, to, reason string) error {
	snapshot, err := json.Marshal(appointment)
	if err != nil {
		return err
	}
	return repository.CreateEvent(ctx, &types.LeadAppointmentEvent{AppointmentID: appointment.ID, LeadID: appointment.LeadID, EventType: eventType, ActorUserID: actor, ActorRole: role, FromStatus: from, ToStatus: to, SnapshotJSON: string(snapshot), Reason: reason, CreatedAt: time.Now()})
}

func appendConflict(items []types.AppointmentConflict, conflict types.AppointmentConflict) []types.AppointmentConflict {
	for _, item := range items {
		if item.Code == conflict.Code && item.AppointmentID == conflict.AppointmentID {
			return items
		}
	}
	return append(items, conflict)
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func (uc *AppointmentUseCase) bindingName(ctx context.Context, userID string) string {
	if userID == "" || uc.bindings == nil {
		return ""
	}
	binding, err := uc.bindings.GetByWeComUserID(ctx, userID)
	if err != nil || binding == nil {
		return ""
	}
	return binding.EmployeeName
}

func (uc *AppointmentUseCase) withTransaction(ctx context.Context, fn func(context.Context, repo.AppointmentRepository, repo.CloserAvailabilityRepository, repo.LeadRepository, repo.UserBindingRepository, repo.LeadReminderRepository) error) error {
	if uc.txManager == nil {
		return fn(ctx, uc.appointments, uc.availability, uc.leads, uc.bindings, uc.reminders)
	}
	return uc.txManager.WithinTransaction(ctx, func(ctx context.Context, repos repo.TransactionRepositories) error {
		return fn(ctx, repos.AppointmentRepository(), repos.CloserAvailabilityRepository(), repos.LeadRepository(), repos.UserBindingRepository(), repos.LeadReminderRepository())
	})
}

func (uc *AppointmentUseCase) withDeliveryTransaction(ctx context.Context, fn func(context.Context, repo.AppointmentRepository, repo.LeadRepository, repo.LeadActivityRepository, repo.LeadReminderRepository) error) error {
	if uc.txManager == nil {
		return fn(ctx, uc.appointments, uc.leads, uc.activities, uc.reminders)
	}
	return uc.txManager.WithinTransaction(ctx, func(ctx context.Context, repos repo.TransactionRepositories) error {
		return fn(ctx, repos.AppointmentRepository(), repos.LeadRepository(), repos.LeadActivityRepository(), repos.LeadReminderRepository())
	})
}

type AvailabilityUseCase struct {
	repository repo.CloserAvailabilityRepository
	bindings   repo.UserBindingRepository
	flags      runtime.WorkflowFeatureFlags
}

func NewAvailabilityUseCase(repository repo.CloserAvailabilityRepository, bindings repo.UserBindingRepository, flags runtime.WorkflowFeatureFlags) *AvailabilityUseCase {
	return &AvailabilityUseCase{repository: repository, bindings: bindings, flags: flags}
}

func (uc *AvailabilityUseCase) Create(ctx context.Context, item *types.CloserAvailability, actor, role string) error {
	if uc.flags == nil || !uc.flags.WorkflowV2Enabled(ctx) || !uc.flags.AppointmentEnabled(ctx) {
		return types.ErrAppointmentForbidden
	}
	if role != string(types.UserRoleManager) && role != string(types.UserRoleAdmin) {
		return types.ErrUnauthorized
	}
	if err := validateAssignableCloser(ctx, uc.bindings, item.CloserUserID); err != nil {
		return err
	}
	if item.StartAt.IsZero() || !item.EndAt.After(item.StartAt) || (item.Kind != types.AvailabilityKindWorking && item.Kind != types.AvailabilityKindUnavailable) || item.Capacity < 1 {
		return types.ErrAvailabilityInvalid
	}
	now := time.Now()
	item.StartAt, item.EndAt = item.StartAt.UTC(), item.EndAt.UTC()
	item.CreatedBy, item.CreatedAt, item.UpdatedAt = actor, now, now
	return uc.repository.Create(ctx, item)
}

func (uc *AvailabilityUseCase) List(ctx context.Context, closer string, startAt, endAt time.Time, actor, role string) ([]*types.CloserAvailability, error) {
	if uc.flags == nil || !uc.flags.WorkflowV2Enabled(ctx) || !uc.flags.AppointmentEnabled(ctx) {
		return nil, types.ErrAppointmentForbidden
	}
	if role == string(types.UserRoleCloser) {
		closer = actor
	}
	if role != string(types.UserRoleManager) && role != string(types.UserRoleAdmin) && role != string(types.UserRoleCloser) {
		return nil, types.ErrUnauthorized
	}
	return uc.repository.List(ctx, closer, startAt, endAt)
}

func (uc *AvailabilityUseCase) Delete(ctx context.Context, id int64, actor, role string) error {
	if uc.flags == nil || !uc.flags.WorkflowV2Enabled(ctx) || !uc.flags.AppointmentEnabled(ctx) {
		return types.ErrAppointmentForbidden
	}
	if role != string(types.UserRoleManager) && role != string(types.UserRoleAdmin) {
		return types.ErrUnauthorized
	}
	deleted, err := uc.repository.Delete(ctx, id, "")
	if err != nil {
		return err
	}
	if !deleted {
		return types.ErrAppointmentNotFound
	}
	return nil
}

func (uc *AppointmentUseCase) scheduleAppointmentReminders(ctx context.Context, appointment *types.LeadAppointment) error {
	lead, err := uc.leads.GetByID(ctx, appointment.LeadID)
	if err != nil {
		return err
	}
	return writeAppointmentReminders(ctx, uc.reminders, appointment, lead)
}
