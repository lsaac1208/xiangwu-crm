-- Immutable CMS audit events for workflow, settings, permissions and publishing.

-- +kxone dialect sqlite
CREATE TABLE IF NOT EXISTS cms_audit_logs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	action varchar(64) NOT NULL,
	object_type varchar(64) NOT NULL,
	object_id varchar(128) NOT NULL,
	actor_id varchar(128) NOT NULL,
	metadata text,
	created_at datetime NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_cms_audit_logs_action ON cms_audit_logs(action);
CREATE INDEX IF NOT EXISTS idx_cms_audit_logs_actor_id ON cms_audit_logs(actor_id);
CREATE INDEX IF NOT EXISTS idx_cms_audit_logs_created_at ON cms_audit_logs(created_at);
CREATE INDEX IF NOT EXISTS idx_cms_audit_object ON cms_audit_logs(object_type, object_id);

-- +kxone dialect mysql
CREATE TABLE IF NOT EXISTS cms_audit_logs (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	action varchar(64) NOT NULL,
	object_type varchar(64) NOT NULL,
	object_id varchar(128) NOT NULL,
	actor_id varchar(128) NOT NULL,
	metadata text,
	created_at datetime NOT NULL,
	KEY idx_cms_audit_logs_action (action),
	KEY idx_cms_audit_logs_actor_id (actor_id),
	KEY idx_cms_audit_logs_created_at (created_at),
	KEY idx_cms_audit_object (object_type, object_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
