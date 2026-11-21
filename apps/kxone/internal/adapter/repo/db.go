package repo

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/lsaac128/kxone/internal/config"
	"github.com/lsaac128/kxone/internal/security"
	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// NewDB 根据配置创建数据库连接
func NewDB(cfg *config.DatabaseConfig) (*gorm.DB, error) {
	var dialector gorm.Dialector
	maxOpenConns := cfg.MaxConns
	if maxOpenConns <= 0 {
		maxOpenConns = 1
	}

	switch cfg.Driver {
	case "sqlite":
		// PRAGMAs in the DSN are applied to every pooled connection. WAL lets
		// readers proceed during writes, busy_timeout absorbs short write bursts,
		// and foreign_keys prevents silent referential-integrity violations.
		dialector = sqlite.Open(withSQLitePragmas(cfg.Database))
		if maxOpenConns > 4 {
			maxOpenConns = 4
		}
	case "mysql":
		// loc=UTC 避免依赖容器系统时区，确保跨节点时间字段解析一致。
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=UTC",
			cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database)
		dialector = mysql.Open(dsn)
	default:
		return nil, fmt.Errorf("unsupported database driver: %s", cfg.Driver)
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: logger.New(log.New(os.Stderr, "", log.LstdFlags), logger.Config{
			LogLevel:             gormLogLevel(cfg.LogLevel),
			ParameterizedQueries: true,
		}),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB: %w", err)
	}

	sqlDB.SetMaxOpenConns(maxOpenConns)
	maxIdleConns := maxOpenConns / 2
	if maxIdleConns < 1 {
		maxIdleConns = 1
	}
	sqlDB.SetMaxIdleConns(maxIdleConns)
	// 防止数据库端主动关闭空闲连接后本端复用僵尸连接
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	sqlDB.SetConnMaxIdleTime(10 * time.Minute)

	return db, nil
}

func withSQLitePragmas(dsn string) string {
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	return dsn + separator + "_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on&_txlock=immediate"
}

func gormLogLevel(level string) logger.LogLevel {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "silent":
		return logger.Silent
	case "error":
		return logger.Error
	case "warn", "warning":
		return logger.Warn
	case "info":
		return logger.Info
	default:
		return logger.Warn
	}
}

// AutoMigrate 执行数据库迁移
func AutoMigrate(db *gorm.DB) error {
	models := []any{
		&LeadModel{},
		&security.Session{},
		&LeadCreateRequest{},
		&FollowupRequest{},
		&AccessAudit{},
		&LeadDeletionEventModel{},
		&LeadDispatchRecordModel{},
		&LeadFollowupModel{},
		&WeComCardTaskModel{},
		&WeComCallbackLogModel{},
	}
	// SQLite's schema parser cannot round-trip the partial unique index created
	// by migration 000023. Once the user table exists, SQL migrations are the
	// source of truth for that table; skipping it keeps repeated development
	// startups idempotent without touching the production migration path.
	if db.Dialector.Name() != "sqlite" || !db.Migrator().HasTable(&WeComUserBindingModel{}) {
		models = append(models, &WeComUserBindingModel{})
	}
	models = append(models,
		&WeComUserBindingRequestModel{},
		&SystemSettingModel{},
		&ChannelIntegrationModel{},
		&ChannelIntegrationAuditModel{},
		&LeadAppointmentModel{},
		&LeadAppointmentEventModel{},
		&CloserAvailabilityModel{},
		&LeadPoolRecordModel{},
		&LeadActivityRecordModel{},
		&LeadReminderRecordModel{},
		&GroupRobotNotificationModel{},
	)
	return db.AutoMigrate(models...)
}
