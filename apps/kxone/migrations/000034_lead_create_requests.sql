CREATE TABLE IF NOT EXISTS crm_lead_create_requests (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 account_id BIGINT NOT NULL,
 endpoint VARCHAR(64) NOT NULL,
 request_key VARCHAR(36) NOT NULL,
 fingerprint VARCHAR(64) NOT NULL,
 lead_id BIGINT NOT NULL DEFAULT 0,
 outcome VARCHAR(32) NOT NULL DEFAULT '',
 created_at DATETIME NOT NULL,
 UNIQUE(account_id,endpoint,request_key)
);
