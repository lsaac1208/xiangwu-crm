-- Initial schema for empty KXOne databases.
-- Dialect blocks are selected by the application migration runner.

-- +kxone dialect sqlite
CREATE TABLE IF NOT EXISTS leads (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	lead_no varchar(64) NOT NULL,
	customer_name varchar(64),
	customer_mobile varchar(512),
	customer_mobile_masked varchar(32),
	city varchar(64),
	product_name varchar(128),
	source_channel varchar(128),
	source_campaign varchar(512),
	lead_level varchar(32),
	status varchar(64) NOT NULL,
	version integer NOT NULL DEFAULT 0,
	dispatch_manager_user_id varchar(128),
	assigned_sales_user_id varchar(128),
	owner_user_id varchar(128),
	created_at datetime NOT NULL,
	assigned_at datetime,
	claimed_at datetime,
	first_followup_at datetime,
	converted_at datetime,
	closed_at datetime,
	deal_amount real NOT NULL DEFAULT 0,
	close_reason varchar(255),
	updated_at datetime NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_leads_lead_no ON leads(lead_no);
CREATE INDEX IF NOT EXISTS idx_leads_status ON leads(status);
CREATE INDEX IF NOT EXISTS idx_leads_dispatch_manager_user_id ON leads(dispatch_manager_user_id);
CREATE INDEX IF NOT EXISTS idx_leads_assigned_sales_user_id ON leads(assigned_sales_user_id);
CREATE INDEX IF NOT EXISTS idx_leads_owner_user_id ON leads(owner_user_id);
CREATE INDEX IF NOT EXISTS idx_leads_created_at ON leads(created_at);
CREATE INDEX IF NOT EXISTS idx_leads_assigned_at ON leads(assigned_at);
CREATE INDEX IF NOT EXISTS idx_leads_converted_at ON leads(converted_at);
CREATE INDEX IF NOT EXISTS idx_leads_closed_at ON leads(closed_at);
CREATE INDEX IF NOT EXISTS idx_leads_updated_at ON leads(updated_at);
CREATE INDEX IF NOT EXISTS idx_leads_status_created ON leads(status, created_at);
CREATE INDEX IF NOT EXISTS idx_leads_status_assigned ON leads(status, assigned_at);
CREATE INDEX IF NOT EXISTS idx_leads_status_updated ON leads(status, updated_at);
CREATE INDEX IF NOT EXISTS idx_leads_owner_status_updated ON leads(owner_user_id, status, updated_at);
CREATE INDEX IF NOT EXISTS idx_leads_assigned_status_updated ON leads(assigned_sales_user_id, status, updated_at);

CREATE TABLE IF NOT EXISTS lead_dispatch_records (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	lead_id integer NOT NULL,
	manager_user_id varchar(128) NOT NULL,
	sales_user_id varchar(128) NOT NULL,
	remark varchar(512),
	action_type varchar(64) NOT NULL,
	created_at datetime NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_lead_dispatch_records_lead_id ON lead_dispatch_records(lead_id);
CREATE INDEX IF NOT EXISTS idx_lead_dispatch_records_created_at ON lead_dispatch_records(created_at);

CREATE TABLE IF NOT EXISTS lead_followups (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	lead_id integer NOT NULL,
	sales_user_id varchar(128) NOT NULL,
	followup_result varchar(64) NOT NULL,
	intention_level varchar(64),
	remark text NOT NULL,
	next_followup_at datetime,
	is_valid boolean NOT NULL DEFAULT true,
	invalid_reason varchar(128),
	created_at datetime NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_lead_followups_lead_id ON lead_followups(lead_id);
CREATE INDEX IF NOT EXISTS idx_lead_followups_created_at ON lead_followups(created_at);

CREATE TABLE IF NOT EXISTS wecom_card_tasks (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	lead_id integer NOT NULL,
	card_scene varchar(64) NOT NULL,
	receiver_user_id varchar(128) NOT NULL,
	task_id varchar(128) NOT NULL,
	msg_id varchar(128),
	response_code varchar(256),
	card_status varchar(64) NOT NULL,
	retry_count integer NOT NULL DEFAULT 0,
	last_error text,
	next_retry_at datetime,
	sent_at datetime NOT NULL,
	updated_at datetime,
	response_code_expire_at datetime
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_wecom_card_tasks_task_id ON wecom_card_tasks(task_id);
CREATE INDEX IF NOT EXISTS idx_wecom_card_tasks_lead_id ON wecom_card_tasks(lead_id);
CREATE INDEX IF NOT EXISTS idx_wecom_card_tasks_response_code ON wecom_card_tasks(response_code);
CREATE INDEX IF NOT EXISTS idx_wecom_card_tasks_card_status ON wecom_card_tasks(card_status);
CREATE INDEX IF NOT EXISTS idx_wecom_card_tasks_next_retry_at ON wecom_card_tasks(next_retry_at);
CREATE INDEX IF NOT EXISTS idx_wecom_card_tasks_sent_at ON wecom_card_tasks(sent_at);
CREATE INDEX IF NOT EXISTS idx_wecom_card_tasks_response_code_expire_at ON wecom_card_tasks(response_code_expire_at);
CREATE INDEX IF NOT EXISTS idx_card_lead_scene_sent ON wecom_card_tasks(lead_id, card_scene, sent_at);
CREATE INDEX IF NOT EXISTS idx_card_status_retry ON wecom_card_tasks(card_status, next_retry_at);

CREATE TABLE IF NOT EXISTS wecom_callback_logs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	event_id varchar(128),
	task_id varchar(128),
	lead_id integer,
	from_user_id varchar(128),
	event_key varchar(128),
	raw_payload text NOT NULL,
	process_status varchar(64) NOT NULL,
	error_message text,
	created_at datetime NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_wecom_callback_logs_event_id ON wecom_callback_logs(event_id);
CREATE INDEX IF NOT EXISTS idx_wecom_callback_logs_task_id ON wecom_callback_logs(task_id);
CREATE INDEX IF NOT EXISTS idx_wecom_callback_logs_lead_id ON wecom_callback_logs(lead_id);
CREATE INDEX IF NOT EXISTS idx_wecom_callback_logs_from_user_id ON wecom_callback_logs(from_user_id);
CREATE INDEX IF NOT EXISTS idx_wecom_callback_logs_event_key ON wecom_callback_logs(event_key);
CREATE INDEX IF NOT EXISTS idx_wecom_callback_logs_process_status ON wecom_callback_logs(process_status);
CREATE INDEX IF NOT EXISTS idx_wecom_callback_logs_created_at ON wecom_callback_logs(created_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_callback_task_user_event ON wecom_callback_logs(task_id, from_user_id, event_key);

CREATE TABLE IF NOT EXISTS crm_wecom_user_bindings (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	crm_user_id integer NOT NULL,
	wecom_userid varchar(128) NOT NULL,
	employee_name varchar(128),
	role varchar(64) NOT NULL,
	department_id integer,
	status varchar(64) NOT NULL,
	created_at datetime NOT NULL,
	updated_at datetime NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_crm_wecom_user_bindings_crm_user_id ON crm_wecom_user_bindings(crm_user_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_crm_wecom_user_bindings_wecom_userid ON crm_wecom_user_bindings(wecom_userid);
CREATE INDEX IF NOT EXISTS idx_crm_wecom_user_bindings_department_id ON crm_wecom_user_bindings(department_id);
CREATE INDEX IF NOT EXISTS idx_crm_wecom_user_bindings_status ON crm_wecom_user_bindings(status);
CREATE INDEX IF NOT EXISTS idx_crm_wecom_user_bindings_created_at ON crm_wecom_user_bindings(created_at);

CREATE TABLE IF NOT EXISTS crm_wecom_user_binding_requests (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	wecom_userid varchar(128) NOT NULL,
	employee_name varchar(128) NOT NULL,
	mobile varchar(64),
	department varchar(128),
	remark varchar(500),
	status varchar(64) NOT NULL,
	crm_user_id integer,
	role varchar(64),
	department_id integer,
	reject_reason varchar(500),
	reviewed_by varchar(128),
	reviewed_at datetime,
	created_at datetime NOT NULL,
	updated_at datetime NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_crm_wecom_user_binding_requests_wecom_userid ON crm_wecom_user_binding_requests(wecom_userid);
CREATE INDEX IF NOT EXISTS idx_crm_wecom_user_binding_requests_status ON crm_wecom_user_binding_requests(status);
CREATE INDEX IF NOT EXISTS idx_crm_wecom_user_binding_requests_reviewed_at ON crm_wecom_user_binding_requests(reviewed_at);
CREATE INDEX IF NOT EXISTS idx_crm_wecom_user_binding_requests_created_at ON crm_wecom_user_binding_requests(created_at);

CREATE TABLE IF NOT EXISTS system_settings (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	setting_key varchar(128) NOT NULL,
	setting_value text,
	value_type varchar(32) NOT NULL DEFAULT 'string',
	setting_group varchar(64) NOT NULL,
	description varchar(255),
	is_sensitive boolean NOT NULL DEFAULT false,
	updated_by varchar(128),
	created_at datetime NOT NULL,
	updated_at datetime NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_system_settings_setting_key ON system_settings(setting_key);
CREATE INDEX IF NOT EXISTS idx_system_settings_setting_group ON system_settings(setting_group);
CREATE INDEX IF NOT EXISTS idx_system_settings_created_at ON system_settings(created_at);

-- +kxone dialect mysql
CREATE TABLE IF NOT EXISTS leads (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	lead_no varchar(64) NOT NULL,
	customer_name varchar(64),
	customer_mobile varchar(512),
	customer_mobile_masked varchar(32),
	city varchar(64),
	product_name varchar(128),
	source_channel varchar(128),
	source_campaign varchar(512),
	lead_level varchar(32),
	status varchar(64) NOT NULL,
	version int NOT NULL DEFAULT 0,
	dispatch_manager_user_id varchar(128),
	assigned_sales_user_id varchar(128),
	owner_user_id varchar(128),
	created_at datetime NOT NULL,
	assigned_at datetime,
	claimed_at datetime,
	first_followup_at datetime,
	converted_at datetime,
	closed_at datetime,
	deal_amount double NOT NULL DEFAULT 0,
	close_reason varchar(255),
	updated_at datetime NOT NULL,
	UNIQUE KEY idx_leads_lead_no (lead_no),
	KEY idx_leads_status (status),
	KEY idx_leads_dispatch_manager_user_id (dispatch_manager_user_id),
	KEY idx_leads_assigned_sales_user_id (assigned_sales_user_id),
	KEY idx_leads_owner_user_id (owner_user_id),
	KEY idx_leads_created_at (created_at),
	KEY idx_leads_assigned_at (assigned_at),
	KEY idx_leads_converted_at (converted_at),
	KEY idx_leads_closed_at (closed_at),
	KEY idx_leads_updated_at (updated_at),
	KEY idx_leads_status_created (status, created_at),
	KEY idx_leads_status_assigned (status, assigned_at),
	KEY idx_leads_status_updated (status, updated_at),
	KEY idx_leads_owner_status_updated (owner_user_id, status, updated_at),
	KEY idx_leads_assigned_status_updated (assigned_sales_user_id, status, updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS lead_dispatch_records (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	lead_id BIGINT NOT NULL,
	manager_user_id varchar(128) NOT NULL,
	sales_user_id varchar(128) NOT NULL,
	remark varchar(512),
	action_type varchar(64) NOT NULL,
	created_at datetime NOT NULL,
	KEY idx_lead_dispatch_records_lead_id (lead_id),
	KEY idx_lead_dispatch_records_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS lead_followups (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	lead_id BIGINT NOT NULL,
	sales_user_id varchar(128) NOT NULL,
	followup_result varchar(64) NOT NULL,
	intention_level varchar(64),
	remark text NOT NULL,
	next_followup_at datetime,
	is_valid boolean NOT NULL DEFAULT true,
	invalid_reason varchar(128),
	created_at datetime NOT NULL,
	KEY idx_lead_followups_lead_id (lead_id),
	KEY idx_lead_followups_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS wecom_card_tasks (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	lead_id BIGINT NOT NULL,
	card_scene varchar(64) NOT NULL,
	receiver_user_id varchar(128) NOT NULL,
	task_id varchar(128) NOT NULL,
	msg_id varchar(128),
	response_code varchar(256),
	card_status varchar(64) NOT NULL,
	retry_count int NOT NULL DEFAULT 0,
	last_error text,
	next_retry_at datetime,
	sent_at datetime NOT NULL,
	updated_at datetime,
	response_code_expire_at datetime,
	UNIQUE KEY idx_wecom_card_tasks_task_id (task_id),
	KEY idx_wecom_card_tasks_lead_id (lead_id),
	KEY idx_wecom_card_tasks_response_code (response_code),
	KEY idx_wecom_card_tasks_card_status (card_status),
	KEY idx_wecom_card_tasks_next_retry_at (next_retry_at),
	KEY idx_wecom_card_tasks_sent_at (sent_at),
	KEY idx_wecom_card_tasks_response_code_expire_at (response_code_expire_at),
	KEY idx_card_lead_scene_sent (lead_id, card_scene, sent_at),
	KEY idx_card_status_retry (card_status, next_retry_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS wecom_callback_logs (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	event_id varchar(128),
	task_id varchar(128),
	lead_id BIGINT,
	from_user_id varchar(128),
	event_key varchar(128),
	raw_payload text NOT NULL,
	process_status varchar(64) NOT NULL,
	error_message text,
	created_at datetime NOT NULL,
	KEY idx_wecom_callback_logs_event_id (event_id),
	KEY idx_wecom_callback_logs_task_id (task_id),
	KEY idx_wecom_callback_logs_lead_id (lead_id),
	KEY idx_wecom_callback_logs_from_user_id (from_user_id),
	KEY idx_wecom_callback_logs_event_key (event_key),
	KEY idx_wecom_callback_logs_process_status (process_status),
	KEY idx_wecom_callback_logs_created_at (created_at),
	UNIQUE KEY idx_callback_task_user_event (task_id, from_user_id, event_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS crm_wecom_user_bindings (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	crm_user_id BIGINT NOT NULL,
	wecom_userid varchar(128) NOT NULL,
	employee_name varchar(128),
	role varchar(64) NOT NULL,
	department_id BIGINT,
	status varchar(64) NOT NULL,
	created_at datetime NOT NULL,
	updated_at datetime NOT NULL,
	UNIQUE KEY idx_crm_wecom_user_bindings_crm_user_id (crm_user_id),
	UNIQUE KEY idx_crm_wecom_user_bindings_wecom_userid (wecom_userid),
	KEY idx_crm_wecom_user_bindings_department_id (department_id),
	KEY idx_crm_wecom_user_bindings_status (status),
	KEY idx_crm_wecom_user_bindings_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS crm_wecom_user_binding_requests (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	wecom_userid varchar(128) NOT NULL,
	employee_name varchar(128) NOT NULL,
	mobile varchar(64),
	department varchar(128),
	remark varchar(500),
	status varchar(64) NOT NULL,
	crm_user_id BIGINT,
	role varchar(64),
	department_id BIGINT,
	reject_reason varchar(500),
	reviewed_by varchar(128),
	reviewed_at datetime,
	created_at datetime NOT NULL,
	updated_at datetime NOT NULL,
	KEY idx_crm_wecom_user_binding_requests_wecom_userid (wecom_userid),
	KEY idx_crm_wecom_user_binding_requests_status (status),
	KEY idx_crm_wecom_user_binding_requests_reviewed_at (reviewed_at),
	KEY idx_crm_wecom_user_binding_requests_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS system_settings (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	setting_key varchar(128) NOT NULL,
	setting_value text,
	value_type varchar(32) NOT NULL DEFAULT 'string',
	setting_group varchar(64) NOT NULL,
	description varchar(255),
	is_sensitive boolean NOT NULL DEFAULT false,
	updated_by varchar(128),
	created_at datetime NOT NULL,
	updated_at datetime NOT NULL,
	UNIQUE KEY idx_system_settings_setting_key (setting_key),
	KEY idx_system_settings_setting_group (setting_group),
	KEY idx_system_settings_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
