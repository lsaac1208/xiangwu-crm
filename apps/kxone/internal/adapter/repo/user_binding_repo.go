package repo

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"strings"
	"time"

	"github.com/lsaac128/kxone/pkg/types"
	"gorm.io/gorm"
)

// UserBindingRepository 用户绑定仓储接口
type UserBindingRepository interface {
	// GetByWeComUserID 通过企业微信 UserID 查询绑定
	GetByWeComUserID(ctx context.Context, wecomUserID string) (*types.WeComUserBinding, error)
	GetByGroupRobotMentionUserID(ctx context.Context, mentionUserID string) (*types.WeComUserBinding, error)

	// GetByCRMUserID 通过 CRM UserID 查询绑定
	GetByCRMUserID(ctx context.Context, crmUserID int64) (*types.WeComUserBinding, error)

	// Create 创建绑定
	Create(ctx context.Context, binding *types.WeComUserBinding) error

	// Update 更新绑定
	Update(ctx context.Context, binding *types.WeComUserBinding) error

	// UpdateStatus 更新状态
	UpdateStatus(ctx context.Context, wecomUserID string, status types.UserStatus) error

	// GetByID 通过 ID 查询绑定
	GetByID(ctx context.Context, id int64) (*types.WeComUserBinding, error)

	// List 查询绑定列表
	List(ctx context.Context, status, role string, offset, limit int) ([]*types.WeComUserBinding, int, error)

	// NextCRMUserID 获取下一个可用 CRM 用户 ID
	NextCRMUserID(ctx context.Context) (int64, error)
}

type UserBindingSearchRepository interface {
	Search(ctx context.Context, keyword, status, role string, offset, limit int) ([]*types.WeComUserBinding, int, error)
}

// GormUserBindingRepository GORM 实现的用户绑定仓储
type GormUserBindingRepository struct {
	db *gorm.DB
}

func NewGormUserBindingRepository(db *gorm.DB) *GormUserBindingRepository {
	return &GormUserBindingRepository{db: db}
}

func (r *GormUserBindingRepository) GetByUsername(ctx context.Context, username string) (*types.WeComUserBinding, error) {
	var model WeComUserBindingModel
	if err := r.db.WithContext(ctx).
		Where("username = ?", strings.TrimSpace(username)).
		First(&model).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return model.ToEntity(), nil
}

// ChangePassword uses the verified identity, session and hash as one atomic guard.
func (r *GormUserBindingRepository) ChangePassword(ctx context.Context, id, authVersion int64, oldHash, newHash string) error {
	result := r.db.WithContext(ctx).Model(&WeComUserBindingModel{}).
		Where("id = ? AND auth_version = ? AND password_hash = ? AND status = ?", id, authVersion, oldHash, string(types.UserStatusActive)).
		Updates(map[string]any{"password_hash": newHash, "auth_version": gorm.Expr("auth_version + 1"), "account_version": gorm.Expr("account_version + 1"), "updated_at": time.Now()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return types.ErrAccountVersionConflict
	}
	return nil
}

func (r *GormUserBindingRepository) GetByWeComUserID(ctx context.Context, wecomUserID string) (*types.WeComUserBinding, error) {
	var model WeComUserBindingModel
	if err := r.db.WithContext(ctx).
		Where("wecom_userid = ?", wecomUserID).
		First(&model).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}

	return model.ToEntity(), nil
}

func (r *GormUserBindingRepository) GetByGroupRobotMentionUserID(ctx context.Context, mentionUserID string) (*types.WeComUserBinding, error) {
	var model WeComUserBindingModel
	err := r.db.WithContext(ctx).
		Where("group_robot_mention_userid = ? AND status = ?", strings.TrimSpace(mentionUserID), string(types.UserStatusActive)).
		First(&model).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return model.ToEntity(), nil
}

func (r *GormUserBindingRepository) GetByCRMUserID(ctx context.Context, crmUserID int64) (*types.WeComUserBinding, error) {
	var model WeComUserBindingModel
	if err := r.db.WithContext(ctx).
		Where("crm_user_id = ?", crmUserID).
		First(&model).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}

	return model.ToEntity(), nil
}

func (r *GormUserBindingRepository) ListByWeComUserIDs(ctx context.Context, wecomUserIDs []string) (map[string]*types.WeComUserBinding, error) {
	unique := make([]string, 0, len(wecomUserIDs))
	seen := make(map[string]struct{}, len(wecomUserIDs))
	for _, id := range wecomUserIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	result := make(map[string]*types.WeComUserBinding, len(unique))
	if len(unique) == 0 {
		return result, nil
	}

	var models []WeComUserBindingModel
	if err := r.db.WithContext(ctx).Where("wecom_userid IN ?", unique).Find(&models).Error; err != nil {
		return nil, err
	}
	for i := range models {
		binding := models[i].ToEntity()
		result[binding.WeComUserID] = binding
	}
	return result, nil
}

func (r *GormUserBindingRepository) GetByID(ctx context.Context, id int64) (*types.WeComUserBinding, error) {
	var model WeComUserBindingModel
	if err := r.db.WithContext(ctx).First(&model, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}

	return model.ToEntity(), nil
}

func (r *GormUserBindingRepository) List(ctx context.Context, status, role string, offset, limit int) ([]*types.WeComUserBinding, int, error) {
	return r.Search(ctx, "", status, role, offset, limit)
}

func (r *GormUserBindingRepository) Search(ctx context.Context, keyword, status, role string, offset, limit int) ([]*types.WeComUserBinding, int, error) {
	var total int64
	query := r.db.WithContext(ctx).Model(&WeComUserBindingModel{})
	keyword = strings.TrimSpace(keyword)
	if keyword != "" {
		escaped := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(keyword)
		pattern := "%" + escaped + "%"
		query = query.Where("employee_name LIKE ? ESCAPE '\\' OR username LIKE ? ESCAPE '\\' OR group_robot_mention_userid LIKE ? ESCAPE '\\'", pattern, pattern, pattern)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if role != "" {
		query = query.Where("role = ?", role)
	}
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var models []WeComUserBindingModel
	if err := query.Order("updated_at DESC").Offset(offset).Limit(limit).Find(&models).Error; err != nil {
		return nil, 0, err
	}

	bindings := make([]*types.WeComUserBinding, len(models))
	for i := range models {
		bindings[i] = models[i].ToEntity()
	}
	return bindings, int(total), nil
}

func (r *GormUserBindingRepository) Create(ctx context.Context, binding *types.WeComUserBinding) error {
	binding.AuthVersion = 1
	binding.AccountVersion = 1
	if binding.CRMUserID <= 0 {
		return r.createWithGeneratedCRMUserID(ctx, binding)
	}
	model := &WeComUserBindingModel{}
	model.FromEntity(binding)

	if err := r.db.WithContext(ctx).Create(model).Error; err != nil {
		return err
	}

	binding.ID = model.ID
	return nil
}

func (r *GormUserBindingRepository) createWithGeneratedCRMUserID(ctx context.Context, binding *types.WeComUserBinding) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		model := &WeComUserBindingModel{}
		model.FromEntity(binding)
		temporaryID, err := randomNegativeID()
		if err != nil {
			return err
		}
		model.CRMUserID = temporaryID
		if err := tx.Create(model).Error; err != nil {
			return err
		}
		if err := tx.Model(model).Update("crm_user_id", model.ID).Error; err != nil {
			return err
		}
		binding.ID = model.ID
		binding.CRMUserID = model.ID
		return nil
	})
}

func randomNegativeID() (int64, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return 0, fmt.Errorf("generate temporary CRM user ID: %w", err)
	}
	value := int64(binary.BigEndian.Uint64(raw[:]) & ((1 << 62) - 1))
	if value == 0 {
		value = 1
	}
	return -value, nil
}

func (r *GormUserBindingRepository) Update(ctx context.Context, binding *types.WeComUserBinding) error {
	if binding.AccountVersion < 1 {
		return types.ErrAccountVersionConflict
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current WeComUserBindingModel
		if err := tx.First(&current, binding.ID).Error; err != nil {
			return err
		}
		if current.AccountVersion != binding.AccountVersion {
			return types.ErrAccountVersionConflict
		}
		if current.CRMUserID != binding.CRMUserID || current.WeComUserID != binding.WeComUserID {
			return fmt.Errorf("account identity is immutable")
		}
		updates := map[string]any{"username": binding.Username, "employee_name": binding.EmployeeName, "role": string(binding.Role), "department_id": binding.DepartmentID, "status": string(binding.Status), "group_robot_mention_userid": binding.GroupRobotMentionUserID, "updated_at": time.Now(), "account_version": gorm.Expr("account_version + 1")}
		securityChanged := current.Username != binding.Username || current.Role != string(binding.Role) || current.DepartmentID != binding.DepartmentID || current.Status != string(binding.Status)
		if binding.PasswordHash != "" {
			updates["password_hash"] = binding.PasswordHash
			securityChanged = true
		}
		if securityChanged {
			updates["auth_version"] = gorm.Expr("auth_version + 1")
		}
		result := tx.Model(&WeComUserBindingModel{}).Where("id = ? AND account_version = ?", binding.ID, binding.AccountVersion).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return types.ErrAccountVersionConflict
		}
		if err := tx.First(&current, binding.ID).Error; err != nil {
			return err
		}
		*binding = *current.ToEntity()
		return nil
	})
}

func (r *GormUserBindingRepository) UpdateStatus(ctx context.Context, wecomUserID string, status types.UserStatus) error {
	return r.db.WithContext(ctx).Model(&WeComUserBindingModel{}).
		Where("wecom_userid = ? AND status <> ?", wecomUserID, string(status)).
		Updates(map[string]any{"status": string(status), "auth_version": gorm.Expr("auth_version + 1"), "account_version": gorm.Expr("account_version + 1"), "updated_at": time.Now()}).Error
}

func (r *GormUserBindingRepository) NextCRMUserID(ctx context.Context) (int64, error) {
	var maxID int64
	if err := r.db.WithContext(ctx).
		Model(&WeComUserBindingModel{}).
		Select("COALESCE(MAX(crm_user_id), 0)").
		Scan(&maxID).Error; err != nil {
		return 0, err
	}
	return maxID + 1, nil
}
