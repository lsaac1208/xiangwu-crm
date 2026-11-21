-- Account-scoped lead list preferences and saved views.

-- +kxone dialect sqlite
CREATE TABLE crm_user_ui_preferences (
  user_id varchar(128) PRIMARY KEY,
  columns_json text NOT NULL DEFAULT '[]',
  density varchar(16) NOT NULL DEFAULT 'comfortable',
  default_view_id integer,
  version integer NOT NULL DEFAULT 1,
  created_at datetime NOT NULL,
  updated_at datetime NOT NULL
);

CREATE TABLE crm_lead_saved_views (
  id integer PRIMARY KEY AUTOINCREMENT,
  user_id varchar(128) NOT NULL,
  name varchar(64) NOT NULL,
  filters_json text NOT NULL DEFAULT '{}',
  sort_by varchar(32) NOT NULL DEFAULT 'updated_at',
  sort_order varchar(4) NOT NULL DEFAULT 'desc',
  version integer NOT NULL DEFAULT 1,
  created_at datetime NOT NULL,
  updated_at datetime NOT NULL,
  UNIQUE(user_id, name)
);
CREATE INDEX idx_crm_lead_saved_views_user_updated ON crm_lead_saved_views(user_id, updated_at DESC);
CREATE INDEX idx_crm_users_role_status_updated ON crm_wecom_user_bindings(role, status, updated_at DESC);

-- +kxone dialect mysql
CREATE TABLE crm_user_ui_preferences (
  user_id varchar(128) PRIMARY KEY,
  columns_json text NOT NULL,
  density varchar(16) NOT NULL DEFAULT 'comfortable',
  default_view_id bigint NULL,
  version int NOT NULL DEFAULT 1,
  created_at datetime(3) NOT NULL,
  updated_at datetime(3) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE crm_lead_saved_views (
  id bigint PRIMARY KEY AUTO_INCREMENT,
  user_id varchar(128) NOT NULL,
  name varchar(64) NOT NULL,
  filters_json text NOT NULL,
  sort_by varchar(32) NOT NULL DEFAULT 'updated_at',
  sort_order varchar(4) NOT NULL DEFAULT 'desc',
  version int NOT NULL DEFAULT 1,
  created_at datetime(3) NOT NULL,
  updated_at datetime(3) NOT NULL,
  UNIQUE KEY uk_crm_lead_saved_views_user_name (user_id, name),
  KEY idx_crm_lead_saved_views_user_updated (user_id, updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE INDEX idx_crm_users_role_status_updated ON crm_wecom_user_bindings(role, status, updated_at);
