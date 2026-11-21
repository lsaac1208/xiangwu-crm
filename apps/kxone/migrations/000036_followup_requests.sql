CREATE TABLE IF NOT EXISTS crm_followup_requests (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 account_id BIGINT NOT NULL,
 lead_id BIGINT NOT NULL,
 request_key VARCHAR(36) NOT NULL,
 fingerprint VARCHAR(64) NOT NULL,
 followup_id BIGINT NOT NULL DEFAULT 0,
 created_at DATETIME NOT NULL,
 UNIQUE(account_id, lead_id, request_key)
);
