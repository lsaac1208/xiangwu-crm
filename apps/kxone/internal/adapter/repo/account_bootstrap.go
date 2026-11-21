package repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lsaac128/kxone/pkg/types"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// EnsureBootstrapAdmin creates the first password-enabled administrator. It is
// a no-op once any password-enabled account exists.
func EnsureBootstrapAdmin(ctx context.Context, db *gorm.DB, username, password, env string) error {
	var count int64
	if err := db.WithContext(ctx).Model(&WeComUserBindingModel{}).
		Where("password_hash <> ''").Count(&count).Error; err != nil {
		return fmt.Errorf("count password accounts: %w", err)
	}
	if count > 0 {
		return nil
	}
	username = strings.TrimSpace(username)
	if env != "prod" && password == "" {
		password = "admin12345"
	}
	if username == "" || len(password) < 8 || len(password) > 72 {
		return fmt.Errorf("CRM_BOOTSTRAP_USERNAME and an 8-72 character CRM_BOOTSTRAP_PASSWORD are required for the first account")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash bootstrap password: %w", err)
	}
	var model WeComUserBindingModel
	err = db.WithContext(ctx).Where("username = ? OR wecom_userid = ?", username, username).First(&model).Error
	now := time.Now()
	if err == nil {
		return db.WithContext(ctx).Model(&model).Updates(map[string]any{
			"username": username, "password_hash": string(hash), "status": string(types.UserStatusActive), "updated_at": now,
		}).Error
	}
	if err != gorm.ErrRecordNotFound {
		return fmt.Errorf("find bootstrap account: %w", err)
	}
	model = WeComUserBindingModel{
		CRMUserID: -time.Now().UnixNano(), Username: username, PasswordHash: string(hash),
		WeComUserID: username, EmployeeName: "系统管理员", Role: string(types.UserRoleAdmin),
		Status: string(types.UserStatusActive), CreatedAt: now, UpdatedAt: now,
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&model).Error; err != nil {
			return err
		}
		return tx.Model(&model).Update("crm_user_id", model.ID).Error
	})
}
