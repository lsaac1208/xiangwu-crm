ALTER TABLE crm_wecom_user_bindings ADD COLUMN auth_version BIGINT NOT NULL DEFAULT 1;
ALTER TABLE crm_wecom_user_bindings ADD COLUMN account_version BIGINT NOT NULL DEFAULT 1;
