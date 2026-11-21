package repo

import (
	"context"
	"strings"
	"time"

	"github.com/lsaac128/kxone/pkg/types"
	"gorm.io/gorm"
)

type AppointmentRepository interface {
	Create(ctx context.Context, appointment *types.LeadAppointment) error
	GetByID(ctx context.Context, id int64) (*types.LeadAppointment, error)
	GetActiveByLead(ctx context.Context, leadID int64) (*types.LeadAppointment, error)
	ListCloserOverlaps(ctx context.Context, closerUserID string, startAt, endAt time.Time, excludeID int64) ([]*types.LeadAppointment, error)
	UpdateCAS(ctx context.Context, appointment *types.LeadAppointment, expectedVersion int) (bool, error)
	CreateEvent(ctx context.Context, event *types.LeadAppointmentEvent) error
	ListEvents(ctx context.Context, appointmentID int64) ([]*types.LeadAppointmentEvent, error)
	List(ctx context.Context, filter AppointmentListFilter) ([]*types.LeadAppointment, error)
}

type AppointmentListFilter struct {
	LeadID        int64
	InviterUserID string
	CloserUserID  string
	Status        string
	StartBefore   *time.Time
	EndAfter      *time.Time
}

type GormAppointmentRepository struct{ db *gorm.DB }

func NewGormAppointmentRepository(db *gorm.DB) *GormAppointmentRepository {
	return &GormAppointmentRepository{db: db}
}

func (r *GormAppointmentRepository) Create(ctx context.Context, appointment *types.LeadAppointment) error {
	model := appointmentModelFromEntity(appointment)
	if err := r.db.WithContext(ctx).Create(model).Error; err != nil {
		return err
	}
	appointment.ID = model.ID
	return nil
}

func (r *GormAppointmentRepository) GetByID(ctx context.Context, id int64) (*types.LeadAppointment, error) {
	var model LeadAppointmentModel
	if err := r.db.WithContext(ctx).First(&model, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return appointmentEntityFromModel(&model), nil
}

func (r *GormAppointmentRepository) GetActiveByLead(ctx context.Context, leadID int64) (*types.LeadAppointment, error) {
	var model LeadAppointmentModel
	err := r.db.WithContext(ctx).
		Where("lead_id = ? AND status IN ?", leadID, []string{
			string(types.AppointmentPendingAssignment),
			string(types.AppointmentScheduled),
		}).
		Order(absoluteTimeOrder(r.db, "start_at")).
		First(&model).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return appointmentEntityFromModel(&model), nil
}

func (r *GormAppointmentRepository) ListCloserOverlaps(ctx context.Context, closerUserID string, startAt, endAt time.Time, excludeID int64) ([]*types.LeadAppointment, error) {
	query := r.db.WithContext(ctx).
		Where("closer_user_id = ? AND status = ?", closerUserID, string(types.AppointmentScheduled)).
		Where(absoluteTimeComparison(r.db, "start_at", "<"), endAt.UTC()).Where(absoluteTimeComparison(r.db, "end_at", ">"), startAt.UTC())
	if excludeID > 0 {
		query = query.Where("id <> ?", excludeID)
	}
	var models []LeadAppointmentModel
	if err := query.Order(absoluteTimeOrder(r.db, "start_at")).Find(&models).Error; err != nil {
		return nil, err
	}
	result := make([]*types.LeadAppointment, len(models))
	for i := range models {
		result[i] = appointmentEntityFromModel(&models[i])
	}
	return result, nil
}

func (r *GormAppointmentRepository) UpdateCAS(ctx context.Context, appointment *types.LeadAppointment, expectedVersion int) (bool, error) {
	model := appointmentModelFromEntity(appointment)
	model.Version = expectedVersion + 1
	result := r.db.WithContext(ctx).
		Model(&LeadAppointmentModel{}).
		Where("id = ? AND version = ?", appointment.ID, expectedVersion).
		Select("*").
		Omit("id", "created_at").
		Updates(model)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected > 0 {
		appointment.Version = model.Version
	}
	return result.RowsAffected > 0, nil
}

func (r *GormAppointmentRepository) CreateEvent(ctx context.Context, event *types.LeadAppointmentEvent) error {
	model := appointmentEventModelFromEntity(event)
	if err := r.db.WithContext(ctx).Create(model).Error; err != nil {
		return err
	}
	event.ID = model.ID
	return nil
}

func (r *GormAppointmentRepository) ListEvents(ctx context.Context, appointmentID int64) ([]*types.LeadAppointmentEvent, error) {
	var models []LeadAppointmentEventModel
	if err := r.db.WithContext(ctx).Where("appointment_id = ?", appointmentID).Order(absoluteTimeSort(r.db, "created_at", "ASC", "id")).Find(&models).Error; err != nil {
		return nil, err
	}
	result := make([]*types.LeadAppointmentEvent, len(models))
	for i := range models {
		result[i] = appointmentEventEntityFromModel(&models[i])
	}
	return result, nil
}

func (r *GormAppointmentRepository) List(ctx context.Context, filter AppointmentListFilter) ([]*types.LeadAppointment, error) {
	query := r.db.WithContext(ctx).Model(&LeadAppointmentModel{})
	if filter.LeadID > 0 {
		query = query.Where("lead_id = ?", filter.LeadID)
	}
	if filter.InviterUserID != "" {
		query = query.Where("inviter_user_id = ?", filter.InviterUserID)
	}
	if filter.CloserUserID != "" {
		query = query.Where("closer_user_id = ?", filter.CloserUserID)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.StartBefore != nil {
		query = query.Where(absoluteTimeComparison(r.db, "start_at", "<"), filter.StartBefore.UTC())
	}
	if filter.EndAfter != nil {
		query = query.Where(absoluteTimeComparison(r.db, "end_at", ">"), filter.EndAfter.UTC())
	}
	var models []LeadAppointmentModel
	if err := query.Order(absoluteTimeOrder(r.db, "start_at")).Find(&models).Error; err != nil {
		return nil, err
	}
	result := make([]*types.LeadAppointment, len(models))
	for i := range models {
		result[i] = appointmentEntityFromModel(&models[i])
	}
	return result, nil
}

type CloserAvailabilityRepository interface {
	Create(ctx context.Context, availability *types.CloserAvailability) error
	ListOverlaps(ctx context.Context, closerUserID string, startAt, endAt time.Time) ([]*types.CloserAvailability, error)
	List(ctx context.Context, closerUserID string, startAt, endAt time.Time) ([]*types.CloserAvailability, error)
	Delete(ctx context.Context, id int64, closerUserID string) (bool, error)
}

type GormCloserAvailabilityRepository struct{ db *gorm.DB }

func NewGormCloserAvailabilityRepository(db *gorm.DB) *GormCloserAvailabilityRepository {
	return &GormCloserAvailabilityRepository{db: db}
}

func (r *GormCloserAvailabilityRepository) Create(ctx context.Context, availability *types.CloserAvailability) error {
	model := availabilityModelFromEntity(availability)
	if err := r.db.WithContext(ctx).Create(model).Error; err != nil {
		return err
	}
	availability.ID = model.ID
	return nil
}

func (r *GormCloserAvailabilityRepository) ListOverlaps(ctx context.Context, closerUserID string, startAt, endAt time.Time) ([]*types.CloserAvailability, error) {
	var models []CloserAvailabilityModel
	if err := r.db.WithContext(ctx).
		Where("closer_user_id = ?", closerUserID).Where(absoluteTimeComparison(r.db, "start_at", "<"), endAt.UTC()).Where(absoluteTimeComparison(r.db, "end_at", ">"), startAt.UTC()).
		Order(absoluteTimeOrder(r.db, "start_at")).Find(&models).Error; err != nil {
		return nil, err
	}
	result := make([]*types.CloserAvailability, len(models))
	for i := range models {
		result[i] = availabilityEntityFromModel(&models[i])
	}
	return result, nil
}

func (r *GormCloserAvailabilityRepository) List(ctx context.Context, closerUserID string, startAt, endAt time.Time) ([]*types.CloserAvailability, error) {
	query := r.db.WithContext(ctx).Model(&CloserAvailabilityModel{}).
		Where(absoluteTimeComparison(r.db, "start_at", "<"), endAt.UTC()).Where(absoluteTimeComparison(r.db, "end_at", ">"), startAt.UTC())
	if closerUserID != "" {
		query = query.Where("closer_user_id = ?", closerUserID)
	}
	var models []CloserAvailabilityModel
	if err := query.Order(absoluteTimeOrder(r.db, "start_at")).Find(&models).Error; err != nil {
		return nil, err
	}
	result := make([]*types.CloserAvailability, len(models))
	for i := range models {
		result[i] = availabilityEntityFromModel(&models[i])
	}
	return result, nil
}

func (r *GormCloserAvailabilityRepository) Delete(ctx context.Context, id int64, closerUserID string) (bool, error) {
	query := r.db.WithContext(ctx).Where("id = ?", id)
	if closerUserID != "" {
		query = query.Where("closer_user_id = ?", closerUserID)
	}
	result := query.Delete(&CloserAvailabilityModel{})
	return result.RowsAffected > 0, result.Error
}

type LeadPoolRepository interface {
	Create(ctx context.Context, record *types.LeadPoolRecord) error
	ListByLead(ctx context.Context, leadID int64) ([]*types.LeadPoolRecord, error)
	GetLatestByLead(ctx context.Context, leadID int64) (*types.LeadPoolRecord, error)
	ListAll(ctx context.Context, startAt, endAt *time.Time, limit int) ([]*types.LeadPoolRecord, error)
}

func (r *GormLeadPoolRepository) ListAll(ctx context.Context, startAt, endAt *time.Time, limit int) ([]*types.LeadPoolRecord, error) {
	query := r.db.WithContext(ctx).Model(&LeadPoolRecordModel{})
	if startAt != nil {
		query = query.Where("created_at >= ?", databaseTimeBoundary(r.db, *startAt))
	}
	if endAt != nil {
		query = query.Where("created_at < ?", databaseTimeBoundary(r.db, *endAt))
	}
	if limit > 0 {
		query = query.Limit(limit)
	}
	var models []LeadPoolRecordModel
	if err := query.Order(absoluteTimeSort(r.db, "created_at", "DESC", "id")).Find(&models).Error; err != nil {
		return nil, err
	}
	result := make([]*types.LeadPoolRecord, len(models))
	for i := range models {
		result[i] = poolRecordEntityFromModel(&models[i])
	}
	return result, nil
}

func (r *GormLeadPoolRepository) GetLatestByLead(ctx context.Context, leadID int64) (*types.LeadPoolRecord, error) {
	var model LeadPoolRecordModel
	if err := r.db.WithContext(ctx).Where("lead_id = ?", leadID).Order("pool_version DESC, id DESC").First(&model).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return poolRecordEntityFromModel(&model), nil
}

type GormLeadPoolRepository struct{ db *gorm.DB }

func NewGormLeadPoolRepository(db *gorm.DB) *GormLeadPoolRepository {
	return &GormLeadPoolRepository{db: db}
}

func (r *GormLeadPoolRepository) Create(ctx context.Context, record *types.LeadPoolRecord) error {
	model := poolRecordModelFromEntity(record)
	if err := r.db.WithContext(ctx).Create(model).Error; err != nil {
		return err
	}
	record.ID = model.ID
	return nil
}

func (r *GormLeadPoolRepository) ListByLead(ctx context.Context, leadID int64) ([]*types.LeadPoolRecord, error) {
	var models []LeadPoolRecordModel
	if err := r.db.WithContext(ctx).Where("lead_id = ?", leadID).Order(absoluteTimeSort(r.db, "created_at", "ASC", "id")).Find(&models).Error; err != nil {
		return nil, err
	}
	result := make([]*types.LeadPoolRecord, len(models))
	for i := range models {
		result[i] = poolRecordEntityFromModel(&models[i])
	}
	return result, nil
}

type LeadActivityRepository interface {
	Create(ctx context.Context, record *types.LeadActivityRecord) error
	ListByLead(ctx context.Context, leadID int64) ([]*types.LeadActivityRecord, error)
}

type GormLeadActivityRepository struct{ db *gorm.DB }

func NewGormLeadActivityRepository(db *gorm.DB) *GormLeadActivityRepository {
	return &GormLeadActivityRepository{db: db}
}

func (r *GormLeadActivityRepository) Create(ctx context.Context, record *types.LeadActivityRecord) error {
	model := activityRecordModelFromEntity(record)
	if err := r.db.WithContext(ctx).Create(model).Error; err != nil {
		return err
	}
	record.ID = model.ID
	return nil
}

func (r *GormLeadActivityRepository) ListByLead(ctx context.Context, leadID int64) ([]*types.LeadActivityRecord, error) {
	var models []LeadActivityRecordModel
	if err := r.db.WithContext(ctx).Where("lead_id = ?", leadID).Order(absoluteTimeSort(r.db, "created_at", "ASC", "id")).Find(&models).Error; err != nil {
		return nil, err
	}
	result := make([]*types.LeadActivityRecord, len(models))
	for i := range models {
		result[i] = activityRecordEntityFromModel(&models[i])
	}
	return result, nil
}

type LeadReminderRepository interface {
	Create(ctx context.Context, record *types.LeadReminderRecord) (bool, error)
	ListDue(ctx context.Context, now time.Time, limit int) ([]*types.LeadReminderRecord, error)
	ListDueByType(ctx context.Context, reminderType string, now time.Time, limit int) ([]*types.LeadReminderRecord, error)
	TryMarkSending(ctx context.Context, id int64) (bool, error)
	MarkResult(ctx context.Context, id int64, status, errorMessage string, sentAt *time.Time) error
	SuppressDue(ctx context.Context, reminderType string, dueBefore time.Time, reason string) (int64, error)
}

func (r *GormLeadReminderRepository) TryMarkSending(ctx context.Context, id int64) (bool, error) {
	now := time.Now()
	result := r.db.WithContext(ctx).Model(&LeadReminderRecordModel{}).
		Where("id = ? AND status = ?", id, types.ReminderStatusPending).
		Updates(map[string]any{"status": types.ReminderStatusSending, "updated_at": now})
	return result.RowsAffected > 0, result.Error
}

func (r *GormLeadReminderRepository) MarkResult(ctx context.Context, id int64, status, errorMessage string, sentAt *time.Time) error {
	return r.db.WithContext(ctx).Model(&LeadReminderRecordModel{}).Where("id = ?", id).Updates(map[string]any{
		"status": status, "error_message": errorMessage, "sent_at": sentAt, "updated_at": time.Now(),
	}).Error
}

type GormLeadReminderRepository struct{ db *gorm.DB }

func NewGormLeadReminderRepository(db *gorm.DB) *GormLeadReminderRepository {
	return &GormLeadReminderRepository{db: db}
}

func (r *GormLeadReminderRepository) Create(ctx context.Context, record *types.LeadReminderRecord) (bool, error) {
	model := reminderRecordModelFromEntity(record)
	result := r.db.WithContext(ctx).Where("idempotency_key = ?", record.IdempotencyKey).FirstOrCreate(model)
	if result.Error != nil {
		return false, result.Error
	}
	record.ID = model.ID
	return result.RowsAffected > 0, nil
}

func (r *GormLeadReminderRepository) ListDue(ctx context.Context, now time.Time, limit int) ([]*types.LeadReminderRecord, error) {
	return r.listDue(ctx, "", now, limit)
}

func (r *GormLeadReminderRepository) ListDueByType(ctx context.Context, reminderType string, now time.Time, limit int) ([]*types.LeadReminderRecord, error) {
	return r.listDue(ctx, strings.TrimSpace(reminderType), now, limit)
}

func (r *GormLeadReminderRepository) listDue(ctx context.Context, reminderType string, now time.Time, limit int) ([]*types.LeadReminderRecord, error) {
	query := r.db.WithContext(ctx).Where("status = ?", types.ReminderStatusPending).Where(absoluteTimeComparison(r.db, "due_at", "<="), now.UTC())
	if reminderType != "" {
		query = query.Where("reminder_type = ?", reminderType)
	}
	query = query.Order(absoluteTimeOrder(r.db, "due_at"))
	if limit > 0 {
		query = query.Limit(limit)
	}
	var models []LeadReminderRecordModel
	if err := query.Find(&models).Error; err != nil {
		return nil, err
	}
	result := make([]*types.LeadReminderRecord, len(models))
	for i := range models {
		result[i] = reminderRecordEntityFromModel(&models[i])
	}
	return result, nil
}

func (r *GormLeadReminderRepository) SuppressDue(ctx context.Context, reminderType string, dueBefore time.Time, reason string) (int64, error) {
	result := r.db.WithContext(ctx).Model(&LeadReminderRecordModel{}).
		Where("reminder_type = ? AND status = ?", strings.TrimSpace(reminderType), types.ReminderStatusPending).Where(absoluteTimeComparison(r.db, "due_at", "<="), dueBefore.UTC()).
		Updates(map[string]any{
			"status":        types.ReminderStatusSuppressed,
			"error_message": reason,
			"updated_at":    time.Now(),
		})
	return result.RowsAffected, result.Error
}

// SuppressCreatedBefore prevents reminders produced by pre-rollout business
// events from being delivered later merely because their due time is newer.
func (r *GormLeadReminderRepository) SuppressCreatedBefore(ctx context.Context, reminderType string, createdBefore time.Time, reason string) (int64, error) {
	result := r.db.WithContext(ctx).Model(&LeadReminderRecordModel{}).
		Where("reminder_type = ? AND status = ? AND created_at < ?", strings.TrimSpace(reminderType), types.ReminderStatusPending, createdBefore).
		Updates(map[string]any{
			"status":        types.ReminderStatusSuppressed,
			"error_message": reason,
			"updated_at":    time.Now(),
		})
	return result.RowsAffected, result.Error
}

func (r *GormLeadReminderRepository) GetByID(ctx context.Context, id int64) (*types.LeadReminderRecord, error) {
	var model LeadReminderRecordModel
	if err := r.db.WithContext(ctx).First(&model, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return reminderRecordEntityFromModel(&model), nil
}

func appointmentModelFromEntity(e *types.LeadAppointment) *LeadAppointmentModel {
	return &LeadAppointmentModel{ID: e.ID, AppointmentNo: e.AppointmentNo, LeadID: e.LeadID, InviterUserID: e.InviterUserID, CloserUserID: e.CloserUserID, AssignedByUserID: e.AssignedByUserID, StartAt: e.StartAt, EndAt: e.EndAt, BusinessTimezone: e.BusinessTimezone, Campus: e.Campus, VisitorCount: e.VisitorCount, CustomerConcerns: e.CustomerConcerns, HandoffNotes: e.HandoffNotes, SpecialNotes: e.SpecialNotes, Status: string(e.Status), ArrivedAt: e.ArrivedAt, CancelledReason: e.CancelledReason, NoShowReason: e.NoShowReason, RescheduledReason: e.RescheduledReason, Version: e.Version, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt}
}

func appointmentEntityFromModel(m *LeadAppointmentModel) *types.LeadAppointment {
	return &types.LeadAppointment{ID: m.ID, AppointmentNo: m.AppointmentNo, LeadID: m.LeadID, InviterUserID: m.InviterUserID, CloserUserID: m.CloserUserID, AssignedByUserID: m.AssignedByUserID, StartAt: m.StartAt, EndAt: m.EndAt, BusinessTimezone: m.BusinessTimezone, Campus: m.Campus, VisitorCount: m.VisitorCount, CustomerConcerns: m.CustomerConcerns, HandoffNotes: m.HandoffNotes, SpecialNotes: m.SpecialNotes, Status: types.AppointmentStatus(m.Status), ArrivedAt: m.ArrivedAt, CancelledReason: m.CancelledReason, NoShowReason: m.NoShowReason, RescheduledReason: m.RescheduledReason, Version: m.Version, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt}
}

func appointmentEventModelFromEntity(e *types.LeadAppointmentEvent) *LeadAppointmentEventModel {
	return &LeadAppointmentEventModel{ID: e.ID, AppointmentID: e.AppointmentID, LeadID: e.LeadID, EventType: e.EventType, ActorUserID: e.ActorUserID, ActorRole: e.ActorRole, FromStatus: e.FromStatus, ToStatus: e.ToStatus, SnapshotJSON: e.SnapshotJSON, Reason: e.Reason, CreatedAt: e.CreatedAt}
}

func appointmentEventEntityFromModel(m *LeadAppointmentEventModel) *types.LeadAppointmentEvent {
	return &types.LeadAppointmentEvent{ID: m.ID, AppointmentID: m.AppointmentID, LeadID: m.LeadID, EventType: m.EventType, ActorUserID: m.ActorUserID, ActorRole: m.ActorRole, FromStatus: m.FromStatus, ToStatus: m.ToStatus, SnapshotJSON: m.SnapshotJSON, Reason: m.Reason, CreatedAt: m.CreatedAt}
}

func availabilityModelFromEntity(e *types.CloserAvailability) *CloserAvailabilityModel {
	return &CloserAvailabilityModel{ID: e.ID, CloserUserID: e.CloserUserID, StartAt: e.StartAt, EndAt: e.EndAt, Campus: e.Campus, Capacity: e.Capacity, Kind: e.Kind, Reason: e.Reason, CreatedBy: e.CreatedBy, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt}
}

func availabilityEntityFromModel(m *CloserAvailabilityModel) *types.CloserAvailability {
	return &types.CloserAvailability{ID: m.ID, CloserUserID: m.CloserUserID, StartAt: m.StartAt, EndAt: m.EndAt, Campus: m.Campus, Capacity: m.Capacity, Kind: m.Kind, Reason: m.Reason, CreatedBy: m.CreatedBy, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt}
}

func poolRecordModelFromEntity(e *types.LeadPoolRecord) *LeadPoolRecordModel {
	return &LeadPoolRecordModel{ID: e.ID, LeadID: e.LeadID, Action: e.Action, Reason: e.Reason, PreviousOwnerUserID: e.PreviousOwnerUserID, ActorUserID: e.ActorUserID, PoolVersion: e.PoolVersion, CreatedAt: e.CreatedAt}
}

func poolRecordEntityFromModel(m *LeadPoolRecordModel) *types.LeadPoolRecord {
	return &types.LeadPoolRecord{ID: m.ID, LeadID: m.LeadID, Action: m.Action, Reason: m.Reason, PreviousOwnerUserID: m.PreviousOwnerUserID, ActorUserID: m.ActorUserID, PoolVersion: m.PoolVersion, CreatedAt: m.CreatedAt}
}

func activityRecordModelFromEntity(e *types.LeadActivityRecord) *LeadActivityRecordModel {
	return &LeadActivityRecordModel{ID: e.ID, LeadID: e.LeadID, ActivityType: e.ActivityType, ActorUserID: e.ActorUserID, ActorRole: e.ActorRole, FromValueJSON: e.FromValueJSON, ToValueJSON: e.ToValueJSON, MetadataJSON: e.MetadataJSON, CreatedAt: e.CreatedAt}
}

func activityRecordEntityFromModel(m *LeadActivityRecordModel) *types.LeadActivityRecord {
	return &types.LeadActivityRecord{ID: m.ID, LeadID: m.LeadID, ActivityType: m.ActivityType, ActorUserID: m.ActorUserID, ActorRole: m.ActorRole, FromValueJSON: m.FromValueJSON, ToValueJSON: m.ToValueJSON, MetadataJSON: m.MetadataJSON, CreatedAt: m.CreatedAt}
}

func reminderRecordModelFromEntity(e *types.LeadReminderRecord) *LeadReminderRecordModel {
	return &LeadReminderRecordModel{ID: e.ID, LeadID: e.LeadID, AppointmentID: e.AppointmentID, ReminderType: e.ReminderType, BusinessVersion: e.BusinessVersion, DueAt: e.DueAt, SentAt: e.SentAt, ReceiverUserID: e.ReceiverUserID, Status: e.Status, ErrorMessage: e.ErrorMessage, IdempotencyKey: e.IdempotencyKey, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt}
}

func reminderRecordEntityFromModel(m *LeadReminderRecordModel) *types.LeadReminderRecord {
	return &types.LeadReminderRecord{ID: m.ID, LeadID: m.LeadID, AppointmentID: m.AppointmentID, ReminderType: m.ReminderType, BusinessVersion: m.BusinessVersion, DueAt: m.DueAt, SentAt: m.SentAt, ReceiverUserID: m.ReceiverUserID, Status: m.Status, ErrorMessage: m.ErrorMessage, IdempotencyKey: m.IdempotencyKey, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt}
}

// Callers supply only compile-time column/operator constants. SQLite may contain
// both old +08:00 and new UTC timestamps; text ordering is not instant ordering.
func absoluteTimeComparison(db *gorm.DB, column, operator string) string {
	if db.Dialector.Name() == "sqlite" {
		return "julianday(" + column + ") " + operator + " julianday(?)"
	}
	return column + " " + operator + " ?"
}
func absoluteTimeOrder(db *gorm.DB, column string) string {
	return absoluteTimeSort(db, column, "ASC", "id")
}

// All arguments are fixed column names or validated sort directions, never raw input.
func absoluteTimeSort(db *gorm.DB, column, direction, id string) string {
	value := absoluteTimeValue(db, column)
	return value + " IS NULL ASC, " + value + " " + direction + ", " + id + " " + direction
}

func absoluteTimeValue(db *gorm.DB, column string) string {
	if db.Dialector.Name() == "sqlite" {
		return "julianday(" + column + ")"
	}
	return column
}
