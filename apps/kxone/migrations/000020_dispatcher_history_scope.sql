-- Speed up permanent dispatcher history checks by actor and first-assignment action.

-- +kxone dialect sqlite
CREATE INDEX IF NOT EXISTS idx_dispatch_actor_action_lead
ON lead_dispatch_records(manager_user_id, action_type, lead_id);

-- +kxone dialect mysql
CREATE INDEX idx_dispatch_actor_action_lead
ON lead_dispatch_records(manager_user_id, action_type, lead_id);
