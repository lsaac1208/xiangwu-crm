-- Manage advertising callback accounts in KXOne while preserving the lead-gateway protocol boundary.

-- +kxone dialect sqlite
CREATE TABLE IF NOT EXISTS channel_integrations (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  platform varchar(32) NOT NULL,
  account_name varchar(128) NOT NULL,
  callback_path varchar(255) NOT NULL,
  auth_mode varchar(32) NOT NULL,
  secret_configured boolean NOT NULL DEFAULT 0,
  enabled boolean NOT NULL DEFAULT 1,
  last_received_at datetime,
  last_status varchar(64),
  last_lead_id bigint,
  last_error text,
  created_by varchar(128) NOT NULL,
  updated_by varchar(128) NOT NULL,
  created_at datetime NOT NULL,
  updated_at datetime NOT NULL,
  UNIQUE(platform, account_name)
);
CREATE INDEX idx_channel_integrations_enabled ON channel_integrations(enabled);
CREATE INDEX idx_channel_integrations_received ON channel_integrations(last_received_at);

CREATE TABLE IF NOT EXISTS channel_integration_audits (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  integration_id bigint NOT NULL,
  action varchar(32) NOT NULL,
  actor_user_id varchar(128) NOT NULL,
  snapshot_json text NOT NULL,
  created_at datetime NOT NULL
);
CREATE INDEX idx_channel_audits_integration ON channel_integration_audits(integration_id);
CREATE INDEX idx_channel_audits_action ON channel_integration_audits(action);
CREATE INDEX idx_channel_audits_created ON channel_integration_audits(created_at);

INSERT OR IGNORE INTO channel_integrations (platform, account_name, callback_path, auth_mode, secret_configured, enabled, created_by, updated_by, created_at, updated_at) VALUES
('baidu', '百度地图', '/api/leads/baidu', 'platform_compatible', 0, 1, 'system', 'system', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
('tencent', '武汉市洪山区襄五教育培训学校', '/api/leads/tencent', 'platform_compatible', 0, 1, 'system', 'system', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
('tencent', '武汉明壹致行教育科技有限公司', '/api/leads/tencent', 'platform_compatible', 0, 1, 'system', 'system', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
('xiaohongshu', '武汉襄五学校', '/api/leads/xiaohongshu', 'hmac_sha1', 1, 1, 'system', 'system', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
('xiaohongshu', '武汉襄五梅老师', '/api/leads/xiaohongshu', 'hmac_sha1', 1, 1, 'system', 'system', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);

-- +kxone dialect mysql
CREATE TABLE IF NOT EXISTS channel_integrations (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  platform varchar(32) NOT NULL,
  account_name varchar(128) NOT NULL,
  callback_path varchar(255) NOT NULL,
  auth_mode varchar(32) NOT NULL,
  secret_configured boolean NOT NULL DEFAULT false,
  enabled boolean NOT NULL DEFAULT true,
  last_received_at datetime,
  last_status varchar(64),
  last_lead_id BIGINT,
  last_error text,
  created_by varchar(128) NOT NULL,
  updated_by varchar(128) NOT NULL,
  created_at datetime NOT NULL,
  updated_at datetime NOT NULL,
  UNIQUE KEY idx_channel_platform_account (platform, account_name),
  KEY idx_channel_integrations_enabled (enabled),
  KEY idx_channel_integrations_received (last_received_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS channel_integration_audits (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  integration_id BIGINT NOT NULL,
  action varchar(32) NOT NULL,
  actor_user_id varchar(128) NOT NULL,
  snapshot_json text NOT NULL,
  created_at datetime NOT NULL,
  KEY idx_channel_audits_integration (integration_id),
  KEY idx_channel_audits_action (action),
  KEY idx_channel_audits_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT IGNORE INTO channel_integrations (platform, account_name, callback_path, auth_mode, secret_configured, enabled, created_by, updated_by, created_at, updated_at) VALUES
('baidu', '百度地图', '/api/leads/baidu', 'platform_compatible', false, true, 'system', 'system', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
('tencent', '武汉市洪山区襄五教育培训学校', '/api/leads/tencent', 'platform_compatible', false, true, 'system', 'system', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
('tencent', '武汉明壹致行教育科技有限公司', '/api/leads/tencent', 'platform_compatible', false, true, 'system', 'system', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
('xiaohongshu', '武汉襄五学校', '/api/leads/xiaohongshu', 'hmac_sha1', true, true, 'system', 'system', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
('xiaohongshu', '武汉襄五梅老师', '/api/leads/xiaohongshu', 'hmac_sha1', true, true, 'system', 'system', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
