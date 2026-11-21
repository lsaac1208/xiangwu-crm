-- +kxone dialect sqlite
ALTER TABLE leads ADD COLUMN deleted_at datetime;
CREATE INDEX IF NOT EXISTS idx_leads_deleted_at ON leads(deleted_at);
CREATE TABLE IF NOT EXISTS lead_deletion_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    lead_id integer NOT NULL,
    lead_no varchar(64) NOT NULL,
    action varchar(16) NOT NULL,
    actor_userid varchar(128) NOT NULL,
    reason varchar(200) NOT NULL,
    occurred_at datetime NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_lead_deletion_events_lead ON lead_deletion_events(lead_id, occurred_at);

-- +kxone dialect mysql
ALTER TABLE leads ADD COLUMN deleted_at datetime NULL;
CREATE INDEX idx_leads_deleted_at ON leads(deleted_at);
CREATE TABLE IF NOT EXISTS lead_deletion_events (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    lead_id BIGINT NOT NULL,
    lead_no varchar(64) NOT NULL,
    action varchar(16) NOT NULL,
    actor_userid varchar(128) NOT NULL,
    reason varchar(200) NOT NULL,
    occurred_at datetime NOT NULL,
    KEY idx_lead_deletion_events_lead (lead_id, occurred_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
