CREATE TABLE IF NOT EXISTS crm_account_sessions (
 sid VARCHAR(36) PRIMARY KEY,
 account_id BIGINT NOT NULL,
 auth_version BIGINT NOT NULL,
 created_at DATETIME NOT NULL,
 expires_at DATETIME NOT NULL,
 revoked_at DATETIME NULL
);
CREATE INDEX IF NOT EXISTS idx_crm_account_sessions_account_id ON crm_account_sessions(account_id);
CREATE INDEX IF NOT EXISTS idx_crm_account_sessions_expires_at ON crm_account_sessions(expires_at);
