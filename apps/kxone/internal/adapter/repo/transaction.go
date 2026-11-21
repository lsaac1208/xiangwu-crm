package repo

import (
	"context"

	"github.com/lsaac128/kxone/internal/security"
	"gorm.io/gorm"
)

type GormTransactionManager struct {
	db             *gorm.DB
	fieldProtector security.FieldProtector
	mobileHasher   security.MobileHasher
}

func (m *GormTransactionManager) WithLeadSecurity(protector security.FieldProtector, hasher security.MobileHasher) *GormTransactionManager {
	m.fieldProtector = protector
	m.mobileHasher = hasher
	return m
}

func NewGormTransactionManager(db *gorm.DB) *GormTransactionManager {
	return &GormTransactionManager{db: db}
}

func (m *GormTransactionManager) WithinTransaction(ctx context.Context, fn func(ctx context.Context, repos TransactionRepositories) error) error {
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(ctx, &gormTransactionRepositories{
			db:              tx,
			xiangwuFormRepo: NewGormXiangwuFormSubmissionRepository(tx),
			leadRepo: NewGormLeadRepository(tx).
				WithFieldProtector(m.fieldProtector).
				WithMobileHasher(m.mobileHasher),
			dispatchRepo:               NewGormDispatchRecordRepository(tx),
			followupRepo:               NewGormFollowupRepository(tx),
			userBindingRepo:            NewGormUserBindingRepository(tx),
			userBindingRequestRepo:     NewGormUserBindingRequestRepository(tx),
			appointmentRepo:            NewGormAppointmentRepository(tx),
			closerAvailabilityRepo:     NewGormCloserAvailabilityRepository(tx),
			leadPoolRepo:               NewGormLeadPoolRepository(tx),
			leadActivityRepo:           NewGormLeadActivityRepository(tx),
			leadReminderRepo:           NewGormLeadReminderRepository(tx),
			systemSettingRepo:          NewGormSystemSettingRepository(tx).WithFieldProtector(m.fieldProtector),
			groupRobotNotificationRepo: NewGormGroupRobotNotificationRepository(tx),
		})
	})
}

type gormTransactionRepositories struct {
	db                         *gorm.DB
	xiangwuFormRepo            XiangwuFormSubmissionRepository
	leadRepo                   LeadRepository
	dispatchRepo               DispatchRecordRepository
	followupRepo               FollowupRepository
	userBindingRepo            UserBindingRepository
	userBindingRequestRepo     UserBindingRequestRepository
	appointmentRepo            AppointmentRepository
	closerAvailabilityRepo     CloserAvailabilityRepository
	leadPoolRepo               LeadPoolRepository
	leadActivityRepo           LeadActivityRepository
	leadReminderRepo           LeadReminderRepository
	systemSettingRepo          SystemSettingRepository
	groupRobotNotificationRepo GroupRobotNotificationRepository
}

func (r *gormTransactionRepositories) XiangwuFormSubmissionRepository() XiangwuFormSubmissionRepository {
	return r.xiangwuFormRepo
}

func (r *gormTransactionRepositories) SystemSettingRepository() SystemSettingRepository {
	return r.systemSettingRepo
}

func (r *gormTransactionRepositories) GroupRobotNotificationRepository() GroupRobotNotificationRepository {
	return r.groupRobotNotificationRepo
}

func (r *gormTransactionRepositories) AppointmentRepository() AppointmentRepository {
	return r.appointmentRepo
}

func (r *gormTransactionRepositories) CloserAvailabilityRepository() CloserAvailabilityRepository {
	return r.closerAvailabilityRepo
}

func (r *gormTransactionRepositories) LeadPoolRepository() LeadPoolRepository {
	return r.leadPoolRepo
}

func (r *gormTransactionRepositories) LeadActivityRepository() LeadActivityRepository {
	return r.leadActivityRepo
}

func (r *gormTransactionRepositories) LeadReminderRepository() LeadReminderRepository {
	return r.leadReminderRepo
}

func (r *gormTransactionRepositories) UserBindingRepository() UserBindingRepository {
	return r.userBindingRepo
}

func (r *gormTransactionRepositories) UserBindingRequestRepository() UserBindingRequestRepository {
	return r.userBindingRequestRepo
}

func (r *gormTransactionRepositories) LeadRepository() LeadRepository {
	return r.leadRepo
}

func (r *gormTransactionRepositories) DispatchRecordRepository() DispatchRecordRepository {
	return r.dispatchRepo
}

func (r *gormTransactionRepositories) FollowupRepository() FollowupRepository {
	return r.followupRepo
}
