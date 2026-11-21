-- Content ownership and governance indexes.

-- +kxone dialect sqlite
ALTER TABLE cms_entries ADD COLUMN owner_user_id varchar(128) NOT NULL DEFAULT '';
ALTER TABLE leads ADD COLUMN content_entry_id integer;
ALTER TABLE leads ADD COLUMN content_revision_id integer;
ALTER TABLE leads ADD COLUMN content_path varchar(512) NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_cms_entries_owner_user_id ON cms_entries(owner_user_id);
CREATE INDEX IF NOT EXISTS idx_cms_audit_action_created ON cms_audit_logs(action, created_at);
CREATE INDEX IF NOT EXISTS idx_cms_audit_actor_created ON cms_audit_logs(actor_id, created_at);

-- +kxone dialect mysql
ALTER TABLE cms_entries ADD COLUMN owner_user_id varchar(128) NOT NULL DEFAULT '';
ALTER TABLE leads ADD COLUMN content_entry_id BIGINT;
ALTER TABLE leads ADD COLUMN content_revision_id BIGINT;
ALTER TABLE leads ADD COLUMN content_path varchar(512) NOT NULL DEFAULT '';
CREATE INDEX idx_cms_entries_owner_user_id ON cms_entries(owner_user_id);
CREATE INDEX idx_cms_audit_action_created ON cms_audit_logs(action, created_at);
CREATE INDEX idx_cms_audit_actor_created ON cms_audit_logs(actor_id, created_at);
