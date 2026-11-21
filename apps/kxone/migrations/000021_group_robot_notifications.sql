-- Durable enterprise-WeChat group robot delivery ledger.

-- +kxone dialect sqlite
CREATE INDEX idx_pool_records_robot_source ON lead_pool_records(action, reason, actor_user_id, created_at, id);
CREATE TABLE IF NOT EXISTS group_robot_notifications (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  event_key varchar(191) NOT NULL UNIQUE,
  category varchar(32) NOT NULL,
  event_type varchar(64) NOT NULL,
  lead_id integer NOT NULL,
  reminder_id integer,
  business_version integer NOT NULL DEFAULT 0,
  due_at datetime NOT NULL,
  status varchar(32) NOT NULL,
  attempts integer NOT NULL DEFAULT 0,
  next_retry_at datetime,
  last_error text,
  sent_at datetime,
  payload_json text,
  created_at datetime NOT NULL,
  updated_at datetime NOT NULL
);
CREATE INDEX idx_group_robot_ready ON group_robot_notifications(status, due_at);
CREATE INDEX idx_group_robot_category ON group_robot_notifications(category);
CREATE INDEX idx_group_robot_event_type ON group_robot_notifications(event_type);
CREATE INDEX idx_group_robot_source_event ON group_robot_notifications(event_type, lead_id, business_version);
CREATE INDEX idx_group_robot_lead ON group_robot_notifications(lead_id);
CREATE INDEX idx_group_robot_reminder ON group_robot_notifications(reminder_id);
CREATE INDEX idx_group_robot_next_retry ON group_robot_notifications(next_retry_at);

-- +kxone dialect mysql
CREATE INDEX idx_pool_records_robot_source ON lead_pool_records(action, reason, actor_user_id, created_at, id);
CREATE TABLE IF NOT EXISTS group_robot_notifications (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  event_key varchar(191) NOT NULL,
  category varchar(32) NOT NULL,
  event_type varchar(64) NOT NULL,
  lead_id BIGINT NOT NULL,
  reminder_id BIGINT,
  business_version int NOT NULL DEFAULT 0,
  due_at datetime NOT NULL,
  status varchar(32) NOT NULL,
  attempts int NOT NULL DEFAULT 0,
  next_retry_at datetime,
  last_error text,
  sent_at datetime,
  payload_json text,
  created_at datetime NOT NULL,
  updated_at datetime NOT NULL,
  UNIQUE KEY idx_group_robot_event_key (event_key),
  KEY idx_group_robot_ready (status, due_at),
  KEY idx_group_robot_category (category),
  KEY idx_group_robot_event_type (event_type),
  KEY idx_group_robot_source_event (event_type, lead_id, business_version),
  KEY idx_group_robot_lead (lead_id),
  KEY idx_group_robot_reminder (reminder_id),
  KEY idx_group_robot_next_retry (next_retry_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
