-- Record the authenticated CRM user and role that created a lead.

-- +kxone dialect sqlite
ALTER TABLE leads ADD COLUMN created_by_user_id varchar(128);
ALTER TABLE leads ADD COLUMN created_by_role varchar(64);
CREATE INDEX idx_leads_created_by_user_id ON leads(created_by_user_id);
CREATE INDEX idx_leads_created_by_role ON leads(created_by_role);

-- +kxone dialect mysql
ALTER TABLE leads ADD COLUMN created_by_user_id varchar(128), ADD COLUMN created_by_role varchar(64);
CREATE INDEX idx_leads_created_by_user_id ON leads(created_by_user_id);
CREATE INDEX idx_leads_created_by_role ON leads(created_by_role);
