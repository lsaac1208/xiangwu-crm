-- Independent enterprise-WeChat group robot mention identity.

-- +kxone dialect sqlite
ALTER TABLE crm_wecom_user_bindings ADD COLUMN group_robot_mention_userid varchar(128);
CREATE UNIQUE INDEX idx_crm_users_group_robot_mention_userid_active
  ON crm_wecom_user_bindings(group_robot_mention_userid)
  WHERE status = 'active' AND group_robot_mention_userid IS NOT NULL AND group_robot_mention_userid <> '';

-- +kxone dialect mysql
ALTER TABLE crm_wecom_user_bindings ADD COLUMN group_robot_mention_userid varchar(128) NULL;
-- MySQL has no portable partial unique index; application validation enforces
-- uniqueness among active users while allowing disabled accounts to retain IDs.
CREATE INDEX idx_crm_users_group_robot_mention_userid ON crm_wecom_user_bindings(group_robot_mention_userid);
