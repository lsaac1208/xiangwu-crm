-- Replace WeCom OAuth identity with native CRM username/password credentials.

-- +kxone dialect sqlite
ALTER TABLE crm_wecom_user_bindings ADD COLUMN username varchar(128);
ALTER TABLE crm_wecom_user_bindings ADD COLUMN password_hash varchar(255) NOT NULL DEFAULT '';
UPDATE crm_wecom_user_bindings SET username = wecom_userid WHERE username IS NULL OR username = '';
CREATE UNIQUE INDEX idx_crm_users_username ON crm_wecom_user_bindings(username);

-- +kxone dialect mysql
ALTER TABLE crm_wecom_user_bindings ADD COLUMN username varchar(128), ADD COLUMN password_hash varchar(255) NOT NULL DEFAULT '';
UPDATE crm_wecom_user_bindings SET username = wecom_userid WHERE username IS NULL OR username = '';
CREATE UNIQUE INDEX idx_crm_users_username ON crm_wecom_user_bindings(username);
