CREATE TABLE IF NOT EXISTS crm_access_audits (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 request_id VARCHAR(36) NOT NULL,
 account_id BIGINT NOT NULL,
 role VARCHAR(32) NOT NULL,
 action VARCHAR(128) NOT NULL,
 outcome VARCHAR(16) NOT NULL,
 object_ids TEXT NOT NULL,
 object_count INTEGER NOT NULL,
 aggregate BOOLEAN NOT NULL,
 created_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_crm_access_audits_request_id ON crm_access_audits(request_id);
CREATE INDEX IF NOT EXISTS idx_crm_access_audits_account_id ON crm_access_audits(account_id);
CREATE INDEX IF NOT EXISTS idx_crm_access_audits_created_at ON crm_access_audits(created_at);
