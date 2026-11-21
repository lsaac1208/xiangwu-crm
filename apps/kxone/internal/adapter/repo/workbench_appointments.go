package repo

import (
	"context"
	"time"

	"github.com/lsaac128/kxone/pkg/types"
	"gorm.io/gorm"
)

type WorkbenchAppointmentSnapshot struct {
	Appointments      []*types.LeadAppointment
	PendingAssignment int
	Unconfirmed       int
	Today             int
}

type WorkbenchAppointmentRepository interface {
	BuildWorkbenchAppointmentSnapshot(context.Context, string, types.UserRole, time.Time, int) (*WorkbenchAppointmentSnapshot, error)
}

func (r *GormAppointmentRepository) BuildWorkbenchAppointmentSnapshot(ctx context.Context, userID string, role types.UserRole, now time.Time, limit int) (*WorkbenchAppointmentSnapshot, error) {
	result := &WorkbenchAppointmentSnapshot{}
	manager := role == types.UserRoleAdmin || role == types.UserRoleManager
	query := r.db.WithContext(ctx).Model(&LeadAppointmentModel{}).
		Joins("JOIN leads ON leads.id = lead_appointments.lead_id AND leads.deleted_at IS NULL")
	switch {
	case manager:
	case types.IsInvitationRole(role):
		// During a pending handoff the booking retains its historical inviter
		// until claim. Visibility already belongs to the assigned recipient.
		query = query.Where("CASE WHEN leads.ownership_state = ? AND leads.current_appointment_id = lead_appointments.id THEN leads.assigned_inviter_user_id ELSE lead_appointments.inviter_user_id END = ?", string(types.OwnershipAssignedPendingClaim), userID)
	case role == types.UserRoleCloser:
		query = query.Where("lead_appointments.closer_user_id = ?", userID)
	case role == types.UserRoleDispatcher:
		return result, nil
	default:
		return nil, types.ErrUnauthorized
	}
	count := func(q *gorm.DB) (int, error) { var value int64; err := q.Count(&value).Error; return int(value), err }
	var err error
	if manager {
		result.PendingAssignment, err = count(query.Session(&gorm.Session{}).Where("lead_appointments.status = ?", string(types.AppointmentPendingAssignment)))
		if err != nil {
			return nil, err
		}
	}
	start := "lead_appointments.start_at"
	result.Unconfirmed, err = count(query.Session(&gorm.Session{}).Where("lead_appointments.status = ?", string(types.AppointmentScheduled)).Where(absoluteTimeComparison(r.db, start, "<="), now))
	if err != nil {
		return nil, err
	}
	dayStart := types.BusinessDayStart(now)
	dayEnd := dayStart.Add(24 * time.Hour)
	result.Today, err = count(query.Session(&gorm.Session{}).Where("lead_appointments.status = ?", string(types.AppointmentScheduled)).Where(absoluteTimeComparison(r.db, start, ">="), dayStart).Where(absoluteTimeComparison(r.db, start, "<"), dayEnd))
	if err != nil {
		return nil, err
	}
	tasks := query.Session(&gorm.Session{})
	if manager {
		tasks = tasks.Where("lead_appointments.status = ? OR (lead_appointments.status = ? AND "+absoluteTimeComparison(r.db, start, "<=")+")", string(types.AppointmentPendingAssignment), string(types.AppointmentScheduled), now)
	} else if role == types.UserRoleCloser {
		tasks = tasks.Where("lead_appointments.status = ? AND ("+absoluteTimeComparison(r.db, start, "<=")+" OR ("+absoluteTimeComparison(r.db, start, ">=")+" AND "+absoluteTimeComparison(r.db, start, "<")+"))", string(types.AppointmentScheduled), now, dayStart, dayEnd)
	} else {
		tasks = tasks.Where("lead_appointments.status = ? AND "+absoluteTimeComparison(r.db, start, "<="), string(types.AppointmentScheduled), now)
	}
	if limit > 0 {
		tasks = tasks.Limit(limit)
	}
	var models []LeadAppointmentModel
	err = tasks.Select("lead_appointments.*, CASE WHEN lead_appointments.status = ? AND "+absoluteTimeComparison(r.db, start, ">")+" THEN 1 ELSE 0 END AS task_priority", string(types.AppointmentScheduled), now).
		Order("task_priority ASC").Order(absoluteTimeSort(r.db, start, "ASC", "lead_appointments.id")).Find(&models).Error
	if err != nil {
		return nil, err
	}
	for i := range models {
		result.Appointments = append(result.Appointments, appointmentEntityFromModel(&models[i]))
	}
	return result, nil
}
