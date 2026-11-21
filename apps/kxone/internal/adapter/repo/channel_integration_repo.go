package repo

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/lsaac128/kxone/internal/security"
	"github.com/lsaac128/kxone/pkg/types"
	"gorm.io/gorm"
)

var ErrChannelIntegrationConflict = errors.New("channel integration already exists")

type ChannelIntegrationRepository interface {
	List(ctx context.Context) ([]*types.ChannelIntegration, error)
	GetByID(ctx context.Context, id int64) (*types.ChannelIntegration, error)
	GetByPlatformAccount(ctx context.Context, platform, accountName string) (*types.ChannelIntegration, error)
	CountByPlatform(ctx context.Context, platform string) (int64, error)
	Create(ctx context.Context, integration *types.ChannelIntegration) error
	Update(ctx context.Context, integration *types.ChannelIntegration, action string) error
	Delete(ctx context.Context, id int64, actor string) error
	RecordReceive(ctx context.Context, id int64, status string, leadID *int64, lastError string) error
}

type ChannelIntegrationModel struct {
	ID               int64      `gorm:"primaryKey;autoIncrement"`
	Platform         string     `gorm:"type:varchar(32);not null;uniqueIndex:idx_channel_platform_account,priority:1"`
	AccountName      string     `gorm:"type:varchar(128);not null;uniqueIndex:idx_channel_platform_account,priority:2"`
	CallbackPath     string     `gorm:"type:varchar(255);not null"`
	AuthMode         string     `gorm:"type:varchar(32);not null"`
	SecretConfigured bool       `gorm:"not null;default:false"`
	TokenCiphertext  string     `gorm:"type:text"`
	Enabled          bool       `gorm:"not null;index"`
	LastReceivedAt   *time.Time `gorm:"index"`
	LastStatus       string     `gorm:"type:varchar(64)"`
	LastLeadID       *int64
	LastError        string    `gorm:"type:text"`
	CreatedBy        string    `gorm:"type:varchar(128);not null"`
	UpdatedBy        string    `gorm:"type:varchar(128);not null"`
	CreatedAt        time.Time `gorm:"not null"`
	UpdatedAt        time.Time `gorm:"not null"`
}

func (ChannelIntegrationModel) TableName() string { return "channel_integrations" }

type ChannelIntegrationAuditModel struct {
	ID            int64     `gorm:"primaryKey;autoIncrement"`
	IntegrationID int64     `gorm:"not null;index"`
	Action        string    `gorm:"type:varchar(32);not null;index"`
	ActorUserID   string    `gorm:"type:varchar(128);not null"`
	SnapshotJSON  string    `gorm:"type:text;not null"`
	CreatedAt     time.Time `gorm:"not null;index"`
}

func (ChannelIntegrationAuditModel) TableName() string { return "channel_integration_audits" }

func (m *ChannelIntegrationModel) ToEntity() *types.ChannelIntegration {
	return &types.ChannelIntegration{
		ID: m.ID, Platform: m.Platform, AccountName: m.AccountName, CallbackPath: m.CallbackPath,
		AuthMode: m.AuthMode, SecretConfigured: m.TokenCiphertext != "", Enabled: m.Enabled,
		LastReceivedAt: m.LastReceivedAt, LastStatus: m.LastStatus, LastLeadID: m.LastLeadID,
		LastError: m.LastError, CreatedBy: m.CreatedBy, UpdatedBy: m.UpdatedBy,
		CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

func (m *ChannelIntegrationModel) FromEntity(v *types.ChannelIntegration) {
	m.ID, m.Platform, m.AccountName = v.ID, v.Platform, v.AccountName
	m.CallbackPath, m.AuthMode = v.CallbackPath, v.AuthMode
	m.SecretConfigured, m.Enabled = v.SecretConfigured, v.Enabled
	m.TokenCiphertext = v.WebhookToken
	m.LastReceivedAt, m.LastStatus, m.LastLeadID, m.LastError = v.LastReceivedAt, v.LastStatus, v.LastLeadID, v.LastError
	m.CreatedBy, m.UpdatedBy, m.CreatedAt, m.UpdatedAt = v.CreatedBy, v.UpdatedBy, v.CreatedAt, v.UpdatedAt
}

type GormChannelIntegrationRepository struct {
	db        *gorm.DB
	protector security.FieldProtector
}

func NewGormChannelIntegrationRepository(db *gorm.DB) *GormChannelIntegrationRepository {
	return &GormChannelIntegrationRepository{db: db}
}

func (r *GormChannelIntegrationRepository) WithFieldProtector(protector security.FieldProtector) *GormChannelIntegrationRepository {
	r.protector = protector
	return r
}

func (r *GormChannelIntegrationRepository) toEntity(model *ChannelIntegrationModel, reveal bool) (*types.ChannelIntegration, error) {
	entity := model.ToEntity()
	if reveal && model.TokenCiphertext != "" {
		if r.protector == nil {
			return nil, errors.New("channel integration field protector is required")
		}
		token, err := r.protector.Reveal(model.TokenCiphertext)
		if err != nil {
			return nil, err
		}
		entity.WebhookToken = token
	}
	return entity, nil
}

func (r *GormChannelIntegrationRepository) List(ctx context.Context) ([]*types.ChannelIntegration, error) {
	var models []ChannelIntegrationModel
	if err := r.db.WithContext(ctx).Order("platform ASC, account_name ASC").Find(&models).Error; err != nil {
		return nil, err
	}
	result := make([]*types.ChannelIntegration, len(models))
	for i := range models {
		result[i] = models[i].ToEntity()
	}
	return result, nil
}

func (r *GormChannelIntegrationRepository) GetByID(ctx context.Context, id int64) (*types.ChannelIntegration, error) {
	var model ChannelIntegrationModel
	if err := r.db.WithContext(ctx).First(&model, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return r.toEntity(&model, true)
}

func (r *GormChannelIntegrationRepository) GetByPlatformAccount(ctx context.Context, platform, accountName string) (*types.ChannelIntegration, error) {
	var model ChannelIntegrationModel
	if err := r.db.WithContext(ctx).Where("platform = ? AND account_name = ?", platform, accountName).First(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return r.toEntity(&model, true)
}

func (r *GormChannelIntegrationRepository) CountByPlatform(ctx context.Context, platform string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&ChannelIntegrationModel{}).Where("platform = ?", platform).Count(&count).Error
	return count, err
}

func (r *GormChannelIntegrationRepository) Create(ctx context.Context, v *types.ChannelIntegration) error {
	model := &ChannelIntegrationModel{}
	model.FromEntity(v)
	if model.TokenCiphertext != "" {
		if r.protector == nil {
			return errors.New("channel integration field protector is required")
		}
		protected, err := r.protector.Protect(model.TokenCiphertext)
		if err != nil {
			return err
		}
		model.TokenCiphertext = protected
		model.SecretConfigured = true
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Select("*").Create(model).Error; err != nil {
			return ErrChannelIntegrationConflict
		}
		v.ID = model.ID
		return createChannelAudit(tx, v, "create", v.CreatedBy)
	})
}

func (r *GormChannelIntegrationRepository) Update(ctx context.Context, v *types.ChannelIntegration, action string) error {
	model := &ChannelIntegrationModel{}
	model.FromEntity(v)
	if model.TokenCiphertext != "" {
		if r.protector == nil {
			return errors.New("channel integration field protector is required")
		}
		protected, err := r.protector.Protect(model.TokenCiphertext)
		if err != nil {
			return err
		}
		model.TokenCiphertext = protected
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&ChannelIntegrationModel{}).Where("id = ?", v.ID).Updates(map[string]any{
			"platform": v.Platform, "account_name": v.AccountName, "callback_path": v.CallbackPath,
			"auth_mode": v.AuthMode, "secret_configured": model.TokenCiphertext != "", "token_ciphertext": model.TokenCiphertext, "enabled": v.Enabled,
			"updated_by": v.UpdatedBy, "updated_at": v.UpdatedAt,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return createChannelAudit(tx, v, action, v.UpdatedBy)
	})
}

func (r *GormChannelIntegrationRepository) Delete(ctx context.Context, id int64, actor string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var model ChannelIntegrationModel
		if err := tx.First(&model, id).Error; err != nil {
			return err
		}
		item := model.ToEntity()
		if err := createChannelAudit(tx, item, "delete", actor); err != nil {
			return err
		}
		return tx.Delete(&model).Error
	})
}

func (r *GormChannelIntegrationRepository) RecordReceive(ctx context.Context, id int64, status string, leadID *int64, lastError string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&ChannelIntegrationModel{}).Where("id = ?", id).Updates(map[string]any{
		"last_received_at": now, "last_status": status, "last_lead_id": leadID,
		"last_error": lastError, "updated_at": now,
	}).Error
}

func createChannelAudit(tx *gorm.DB, v *types.ChannelIntegration, action, actor string) error {
	snapshot, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return tx.Create(&ChannelIntegrationAuditModel{IntegrationID: v.ID, Action: action, ActorUserID: actor, SnapshotJSON: string(snapshot), CreatedAt: time.Now()}).Error
}
