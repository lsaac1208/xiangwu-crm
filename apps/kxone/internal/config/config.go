package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config 应用配置
type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	Redis    RedisConfig
	AdLead   AdLeadConfig
	JWT      JWTConfig
	Account  AccountConfig
	Security SecurityConfig
	Agent    AgentConfig
}

type AgentConfig struct {
	Enabled   bool
	BaseURL   string
	APIKey    string
	Model     string
	Directory string
}

type AccountConfig struct {
	BootstrapUsername string
	BootstrapPassword string
}

// ServerConfig HTTP 服务配置
type ServerConfig struct {
	Host              string
	Port              string
	Env               string // dev / test / prod
	PublicBaseURL     string
	TrustedProxies    []string
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	HeaderTimeout     time.Duration
	MaxBodyBytes      int64
	MaxHeaderBytes    int
	WorkerMetricsAddr string
	WorkerConcurrency int
}

// SecurityConfig 安全相关配置。
type SecurityConfig struct {
	FieldEncryptionKey string
	MobileHashKey      string
	CORSAllowedOrigins []string
}

// DatabaseConfig 数据库配置
type DatabaseConfig struct {
	Driver        string // sqlite / mysql
	Host          string // MySQL 专用
	Port          int    // MySQL 专用
	User          string // MySQL 专用
	Password      string // MySQL 专用
	Database      string // MySQL: 数据库名, SQLite: 文件路径
	MaxConns      int
	MigrationsDir string
	AutoMigrate   bool
	LogLevel      string
}

// RedisConfig Redis 配置
type RedisConfig struct {
	Host     string
	Port     int
	Password string
	DB       int
}

// AdLeadConfig 广告线索接入配置
type AdLeadConfig struct {
	DefaultManagerUserID string
	WebhookSecret        string
	SignatureTolerance   int // 秒
	TrustedGatewayIPs    []string
	GatewayEnabled       bool
	GatewayHMACSecret    string
	GatewayTolerance     int // 秒
	GatewayPublicBaseURL string
}

// JWTConfig JWT 配置
type JWTConfig struct {
	Secret             string
	AccessTokenExpiry  int // 秒
	RefreshTokenExpiry int // 秒
}

// Load 从环境变量加载配置
func Load() *Config {
	serverEnv := getEnv("SERVER_ENV", "prod")
	return &Config{
		Agent: AgentConfig{Enabled: getEnvBool("AGENT_ENABLED", false), BaseURL: getEnv("AGENT_BASE_URL", ""), APIKey: getEnv("AGENT_API_KEY", ""), Model: getEnv("AGENT_MODEL", "qwen3.8-max"), Directory: getEnv("AGENT_STORAGE_DIR", defaultAgentDirectory(serverEnv))},
		Server: ServerConfig{
			Host:              getEnv("SERVER_HOST", ""),
			Port:              getEnv("SERVER_PORT", "8080"),
			Env:               serverEnv,
			PublicBaseURL:     getFirstEnv([]string{"SERVER_PUBLIC_BASE_URL", "APP_BASE_URL"}, ""),
			TrustedProxies:    getEnvStringSlice("TRUSTED_PROXIES", []string{"127.0.0.1", "::1"}),
			ReadTimeout:       getEnvDuration("SERVER_READ_TIMEOUT", 15*time.Second),
			WriteTimeout:      getEnvDuration("SERVER_WRITE_TIMEOUT", 30*time.Second),
			IdleTimeout:       getEnvDuration("SERVER_IDLE_TIMEOUT", 60*time.Second),
			HeaderTimeout:     getEnvDuration("SERVER_HEADER_TIMEOUT", 5*time.Second),
			MaxBodyBytes:      getEnvInt64("SERVER_MAX_BODY_BYTES", 16<<20),
			MaxHeaderBytes:    getEnvInt("SERVER_MAX_HEADER_BYTES", 1<<20),
			WorkerMetricsAddr: getEnv("WORKER_METRICS_ADDR", "127.0.0.1:9091"),
			WorkerConcurrency: getEnvInt("WORKER_CONCURRENCY", 10),
		},
		Database: DatabaseConfig{
			Driver:        getEnv("DB_DRIVER", "sqlite"),
			Host:          getEnv("DB_HOST", "localhost"),
			Port:          getEnvInt("DB_PORT", 3306),
			User:          getEnv("DB_USER", "root"),
			Password:      getEnv("DB_PASSWORD", ""),
			Database:      getEnv("DB_NAME", "kxone.db"),
			MaxConns:      getEnvInt("DB_MAX_CONNS", 20),
			MigrationsDir: getEnv("MIGRATIONS_DIR", "migrations"),
			AutoMigrate:   getEnvBool("ENABLE_AUTO_MIGRATE", serverEnv != "prod"),
			LogLevel:      getEnv("DB_LOG_LEVEL", defaultDBLogLevel(serverEnv)),
		},
		Redis: RedisConfig{
			Host:     getEnv("REDIS_HOST", "localhost"),
			Port:     getEnvInt("REDIS_PORT", 6379),
			Password: getEnv("REDIS_PASSWORD", ""),
			DB:       getEnvInt("REDIS_DB", 0),
		},
		AdLead: AdLeadConfig{
			DefaultManagerUserID: getEnv("AD_LEAD_DEFAULT_MANAGER_USERID", ""),
			WebhookSecret:        getEnv("AD_LEAD_WEBHOOK_SECRET", ""),
			SignatureTolerance:   getEnvInt("AD_LEAD_SIGNATURE_TOLERANCE", 300),
			TrustedGatewayIPs:    getEnvStringSlice("AD_LEAD_TRUSTED_GATEWAY_IPS", nil),
			GatewayEnabled:       getEnvBool("AD_LEAD_GATEWAY_ENABLED", false),
			GatewayHMACSecret:    getEnv("AD_LEAD_GATEWAY_HMAC_SECRET", ""),
			GatewayTolerance:     getEnvInt("AD_LEAD_GATEWAY_SIGNATURE_TOLERANCE", 300),
			GatewayPublicBaseURL: getEnv("AD_LEAD_GATEWAY_PUBLIC_BASE_URL", "https://leads.whxiangwu.com"),
		},
		JWT: JWTConfig{
			Secret:             getEnv("JWT_SECRET", "change-me-in-production"),
			AccessTokenExpiry:  getEnvInt("JWT_ACCESS_EXPIRY", 7200),    // 2 小时
			RefreshTokenExpiry: getEnvInt("JWT_REFRESH_EXPIRY", 604800), // 7 天
		},
		Account: AccountConfig{
			BootstrapUsername: getEnv("CRM_BOOTSTRAP_USERNAME", "admin"),
			BootstrapPassword: getEnv("CRM_BOOTSTRAP_PASSWORD", ""),
		},
		Security: SecurityConfig{
			FieldEncryptionKey: getFirstEnv([]string{"FIELD_ENCRYPTION_KEY", "PII_ENCRYPTION_KEY"}, ""),
			MobileHashKey:      getEnv("MOBILE_HASH_KEY", ""),
			CORSAllowedOrigins: getEnvStringSlice("CORS_ALLOWED_ORIGINS", nil),
		},
	}
}

func defaultAgentDirectory(env string) string {
	if env == "prod" {
		return "/var/lib/kxone/agent"
	}
	return "data/agent"
}

func (cfg *Config) ValidateProductionSecrets() error {
	if cfg == nil || cfg.Server.Env != "prod" {
		return nil
	}
	if strings.TrimSpace(cfg.Server.PublicBaseURL) == "" {
		return fmt.Errorf("SERVER_PUBLIC_BASE_URL is required in prod")
	}
	if strings.TrimSpace(cfg.JWT.Secret) == "" || cfg.JWT.Secret == "change-me-in-production" {
		return fmt.Errorf("JWT_SECRET must be configured in prod")
	}
	if strings.TrimSpace(cfg.Security.FieldEncryptionKey) == "" {
		return fmt.Errorf("FIELD_ENCRYPTION_KEY is required in prod")
	}
	if strings.TrimSpace(cfg.Security.MobileHashKey) == "" {
		return fmt.Errorf("MOBILE_HASH_KEY is required in prod")
	}
	if strings.TrimSpace(cfg.AdLead.WebhookSecret) == "" {
		return fmt.Errorf("AD_LEAD_WEBHOOK_SECRET is required in prod")
	}
	if strings.TrimSpace(cfg.AdLead.DefaultManagerUserID) == "" {
		return fmt.Errorf("AD_LEAD_DEFAULT_MANAGER_USERID is required in prod")
	}
	if cfg.AdLead.GatewayEnabled && len(strings.TrimSpace(cfg.AdLead.GatewayHMACSecret)) < 32 {
		return fmt.Errorf("AD_LEAD_GATEWAY_HMAC_SECRET must contain at least 32 characters when gateway ingestion is enabled")
	}
	if strings.EqualFold(strings.TrimSpace(cfg.Database.Driver), "sqlite") && !filepath.IsAbs(strings.TrimSpace(cfg.Database.Database)) {
		return fmt.Errorf("DB_NAME must be an absolute path for SQLite in prod")
	}
	return nil
}

func (cfg *Config) MobileHashKeyOrFallback() string {
	if cfg == nil {
		return ""
	}
	if key := strings.TrimSpace(cfg.Security.MobileHashKey); key != "" {
		return key
	}
	return strings.TrimSpace(cfg.Security.FieldEncryptionKey)
}

func defaultDBLogLevel(serverEnv string) string {
	if serverEnv == "prod" {
		return "warn"
	}
	return "info"
}

// getEnv 获取环境变量，带默认值
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvInt 获取整数环境变量，带默认值
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

func getEnvInt64(key string, defaultValue int64) int64 {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.ParseInt(value, 10, 64); err == nil && intVal > 0 {
			return intVal
		}
	}
	return defaultValue
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		if duration, err := time.ParseDuration(value); err == nil && duration > 0 {
			return duration
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		if boolVal, err := strconv.ParseBool(value); err == nil {
			return boolVal
		}
	}
	return defaultValue
}

func getEnvStringSlice(key string, defaultValue []string) []string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue
	}
	if strings.EqualFold(value, "none") {
		return nil
	}

	var result []string
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			result = append(result, item)
		}
	}
	if len(result) == 0 {
		return defaultValue
	}
	return result
}

func getFirstEnv(keys []string, defaultValue string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return defaultValue
}
