-- Additive foundation for invitation, appointment, and negotiation workflow.
-- Legacy columns remain in place throughout the rollout.

ALTER TABLE leads ADD COLUMN business_stage varchar(32);
ALTER TABLE leads ADD COLUMN ownership_state varchar(32);
ALTER TABLE leads ADD COLUMN result_status varchar(32);
ALTER TABLE leads ADD COLUMN star_rating int NOT NULL DEFAULT 0;
ALTER TABLE leads ADD COLUMN assigned_inviter_user_id varchar(128);
ALTER TABLE leads ADD COLUMN inviter_user_id varchar(128);
ALTER TABLE leads ADD COLUMN closer_user_id varchar(128);
ALTER TABLE leads ADD COLUMN current_owner_user_id varchar(128);
ALTER TABLE leads ADD COLUMN current_owner_role varchar(32);
ALTER TABLE leads ADD COLUMN current_appointment_id bigint;
ALTER TABLE leads ADD COLUMN last_followup_at datetime;
ALTER TABLE leads ADD COLUMN next_followup_due_at datetime;
ALTER TABLE leads ADD COLUMN inviter_assigned_at datetime;
ALTER TABLE leads ADD COLUMN inviter_claimed_at datetime;
ALTER TABLE leads ADD COLUMN arrived_at datetime;
ALTER TABLE leads ADD COLUMN delivered_at datetime;
ALTER TABLE leads ADD COLUMN pool_entered_at datetime;
ALTER TABLE leads ADD COLUMN pool_reason varchar(128);

ALTER TABLE lead_followups ADD COLUMN business_stage varchar(32);
ALTER TABLE lead_followups ADD COLUMN actor_role varchar(32);
ALTER TABLE lead_followups ADD COLUMN star_rating int NOT NULL DEFAULT 0;
ALTER TABLE lead_followups ADD COLUMN due_at_snapshot datetime;

UPDATE leads
SET business_stage = CASE
      WHEN status IN ('converted', 'invalid', 'closed') THEN 'finished'
      ELSE 'invitation'
    END,
    ownership_state = CASE
      WHEN status = 'pending_dispatch' THEN 'pending_assignment'
      WHEN status = 'assigned_pending_claim' THEN 'assigned_pending_claim'
      ELSE 'private'
    END,
    result_status = CASE
      WHEN status = 'converted' THEN 'converted'
      WHEN status = 'invalid' THEN 'invalid'
      WHEN status = 'closed' THEN 'closed'
      ELSE 'active'
    END,
    assigned_inviter_user_id = assigned_sales_user_id,
    inviter_user_id = owner_user_id,
    current_owner_user_id = owner_user_id,
    current_owner_role = CASE WHEN owner_user_id IS NOT NULL AND owner_user_id <> '' THEN 'inviter' ELSE '' END,
    inviter_assigned_at = assigned_at,
    inviter_claimed_at = claimed_at
WHERE business_stage IS NULL OR business_stage = '';

UPDATE lead_followups
SET business_stage = 'invitation', actor_role = 'inviter'
WHERE business_stage IS NULL OR business_stage = '';

CREATE INDEX idx_leads_result_status ON leads(result_status);
CREATE INDEX idx_leads_assigned_inviter ON leads(assigned_inviter_user_id);
CREATE INDEX idx_leads_inviter ON leads(inviter_user_id);
CREATE INDEX idx_leads_closer ON leads(closer_user_id);
CREATE INDEX idx_leads_current_appointment ON leads(current_appointment_id);
CREATE INDEX idx_leads_workflow_owner_due ON leads(business_stage, ownership_state, current_owner_user_id, next_followup_due_at);
CREATE INDEX idx_followups_business_stage ON lead_followups(business_stage);

-- +kxone dialect sqlite
CREATE TABLE IF NOT EXISTS lead_appointments (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  appointment_no varchar(64) NOT NULL UNIQUE,
  lead_id integer NOT NULL,
  inviter_user_id varchar(128) NOT NULL,
  closer_user_id varchar(128),
  assigned_by_user_id varchar(128),
  start_at datetime NOT NULL,
  end_at datetime NOT NULL,
  business_timezone varchar(64) NOT NULL,
  campus varchar(128),
  visitor_count integer NOT NULL DEFAULT 1,
  customer_concerns text,
  handoff_notes text,
  special_notes text,
  status varchar(32) NOT NULL,
  arrived_at datetime,
  cancelled_reason varchar(255),
  no_show_reason varchar(255),
  rescheduled_reason varchar(255),
  version integer NOT NULL DEFAULT 0,
  created_at datetime NOT NULL,
  updated_at datetime NOT NULL
);
CREATE INDEX idx_appointments_lead_status ON lead_appointments(lead_id, status);
CREATE INDEX idx_appointments_inviter ON lead_appointments(inviter_user_id);
CREATE INDEX idx_appointments_closer_status_time ON lead_appointments(closer_user_id, status, start_at, end_at);
CREATE INDEX idx_appointments_campus ON lead_appointments(campus);

CREATE TABLE IF NOT EXISTS lead_appointment_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  appointment_id integer NOT NULL,
  lead_id integer NOT NULL,
  event_type varchar(64) NOT NULL,
  actor_user_id varchar(128) NOT NULL,
  actor_role varchar(32) NOT NULL,
  from_status varchar(32),
  to_status varchar(32),
  snapshot_json text NOT NULL,
  reason varchar(255),
  created_at datetime NOT NULL
);
CREATE INDEX idx_appointment_events_appointment ON lead_appointment_events(appointment_id);
CREATE INDEX idx_appointment_events_lead ON lead_appointment_events(lead_id);
CREATE INDEX idx_appointment_events_type ON lead_appointment_events(event_type);
CREATE INDEX idx_appointment_events_created ON lead_appointment_events(created_at);

CREATE TABLE IF NOT EXISTS closer_availability (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  closer_user_id varchar(128) NOT NULL,
  start_at datetime NOT NULL,
  end_at datetime NOT NULL,
  campus varchar(128),
  capacity integer NOT NULL DEFAULT 1,
  kind varchar(32) NOT NULL,
  reason varchar(255),
  created_by varchar(128) NOT NULL,
  created_at datetime NOT NULL,
  updated_at datetime NOT NULL
);
CREATE INDEX idx_closer_availability_time ON closer_availability(closer_user_id, start_at, end_at);
CREATE INDEX idx_closer_availability_campus ON closer_availability(campus);

CREATE TABLE IF NOT EXISTS lead_pool_records (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  lead_id integer NOT NULL,
  action varchar(32) NOT NULL,
  reason varchar(255),
  previous_owner_user_id varchar(128),
  actor_user_id varchar(128),
  pool_version integer NOT NULL,
  created_at datetime NOT NULL
);
CREATE INDEX idx_pool_records_lead_version ON lead_pool_records(lead_id, pool_version);
CREATE INDEX idx_pool_records_action ON lead_pool_records(action);
CREATE INDEX idx_pool_records_created ON lead_pool_records(created_at);

CREATE TABLE IF NOT EXISTS lead_activity_records (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  lead_id integer NOT NULL,
  activity_type varchar(64) NOT NULL,
  actor_user_id varchar(128),
  actor_role varchar(32),
  from_value_json text,
  to_value_json text,
  metadata_json text,
  created_at datetime NOT NULL
);
CREATE INDEX idx_activity_lead_created ON lead_activity_records(lead_id, created_at);
CREATE INDEX idx_activity_type ON lead_activity_records(activity_type);

CREATE TABLE IF NOT EXISTS lead_reminder_records (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  lead_id integer NOT NULL,
  appointment_id integer,
  reminder_type varchar(64) NOT NULL,
  business_version integer NOT NULL,
  due_at datetime NOT NULL,
  sent_at datetime,
  receiver_user_id varchar(128) NOT NULL,
  status varchar(32) NOT NULL,
  error_message text,
  idempotency_key varchar(191) NOT NULL UNIQUE,
  created_at datetime NOT NULL,
  updated_at datetime NOT NULL
);
CREATE INDEX idx_reminders_status_due ON lead_reminder_records(status, due_at, lead_id);
CREATE INDEX idx_reminders_appointment ON lead_reminder_records(appointment_id);

-- +kxone dialect mysql
CREATE TABLE IF NOT EXISTS lead_appointments (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  appointment_no varchar(64) NOT NULL,
  lead_id BIGINT NOT NULL,
  inviter_user_id varchar(128) NOT NULL,
  closer_user_id varchar(128),
  assigned_by_user_id varchar(128),
  start_at datetime NOT NULL,
  end_at datetime NOT NULL,
  business_timezone varchar(64) NOT NULL,
  campus varchar(128),
  visitor_count int NOT NULL DEFAULT 1,
  customer_concerns text,
  handoff_notes text,
  special_notes text,
  status varchar(32) NOT NULL,
  arrived_at datetime,
  cancelled_reason varchar(255),
  no_show_reason varchar(255),
  rescheduled_reason varchar(255),
  version int NOT NULL DEFAULT 0,
  created_at datetime NOT NULL,
  updated_at datetime NOT NULL,
  UNIQUE KEY idx_appointments_no (appointment_no),
  KEY idx_appointments_lead_status (lead_id, status),
  KEY idx_appointments_inviter (inviter_user_id),
  KEY idx_appointments_closer_status_time (closer_user_id, status, start_at, end_at),
  KEY idx_appointments_campus (campus)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS lead_appointment_events (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  appointment_id BIGINT NOT NULL,
  lead_id BIGINT NOT NULL,
  event_type varchar(64) NOT NULL,
  actor_user_id varchar(128) NOT NULL,
  actor_role varchar(32) NOT NULL,
  from_status varchar(32),
  to_status varchar(32),
  snapshot_json text NOT NULL,
  reason varchar(255),
  created_at datetime NOT NULL,
  KEY idx_appointment_events_appointment (appointment_id),
  KEY idx_appointment_events_lead (lead_id),
  KEY idx_appointment_events_type (event_type),
  KEY idx_appointment_events_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS closer_availability (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  closer_user_id varchar(128) NOT NULL,
  start_at datetime NOT NULL,
  end_at datetime NOT NULL,
  campus varchar(128),
  capacity int NOT NULL DEFAULT 1,
  kind varchar(32) NOT NULL,
  reason varchar(255),
  created_by varchar(128) NOT NULL,
  created_at datetime NOT NULL,
  updated_at datetime NOT NULL,
  KEY idx_closer_availability_time (closer_user_id, start_at, end_at),
  KEY idx_closer_availability_campus (campus)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS lead_pool_records (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  lead_id BIGINT NOT NULL,
  action varchar(32) NOT NULL,
  reason varchar(255),
  previous_owner_user_id varchar(128),
  actor_user_id varchar(128),
  pool_version int NOT NULL,
  created_at datetime NOT NULL,
  KEY idx_pool_records_lead_version (lead_id, pool_version),
  KEY idx_pool_records_action (action),
  KEY idx_pool_records_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS lead_activity_records (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  lead_id BIGINT NOT NULL,
  activity_type varchar(64) NOT NULL,
  actor_user_id varchar(128),
  actor_role varchar(32),
  from_value_json text,
  to_value_json text,
  metadata_json text,
  created_at datetime NOT NULL,
  KEY idx_activity_lead_created (lead_id, created_at),
  KEY idx_activity_type (activity_type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS lead_reminder_records (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  lead_id BIGINT NOT NULL,
  appointment_id BIGINT,
  reminder_type varchar(64) NOT NULL,
  business_version int NOT NULL,
  due_at datetime NOT NULL,
  sent_at datetime,
  receiver_user_id varchar(128) NOT NULL,
  status varchar(32) NOT NULL,
  error_message text,
  idempotency_key varchar(191) NOT NULL,
  created_at datetime NOT NULL,
  updated_at datetime NOT NULL,
  UNIQUE KEY idx_reminders_idempotency (idempotency_key),
  KEY idx_reminders_status_due (status, due_at, lead_id),
  KEY idx_reminders_appointment (appointment_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
