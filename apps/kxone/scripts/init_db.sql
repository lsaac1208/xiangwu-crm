-- 数据库初始化脚本（MySQL）
-- 如果使用 SQLite，GORM AutoMigrate 会自动创建表

CREATE DATABASE IF NOT EXISTS kxone CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

USE kxone;

-- leads 表会由 GORM AutoMigrate 创建
-- 以下索引在 GORM 模型中已定义，此处仅作文档参考

-- 复合索引（针对查询优化）
-- CREATE INDEX idx_leads_status_created ON leads(status, created_at);
-- CREATE INDEX idx_leads_status_assigned ON leads(status, assigned_at);
-- CREATE INDEX idx_leads_owner_status ON leads(owner_userid, status, updated_at);

-- 企业微信卡片任务表索引
-- CREATE INDEX idx_card_lead_scene_sent ON wecom_card_tasks(lead_id, card_scene, sent_at DESC);
-- CREATE INDEX idx_card_retry ON wecom_card_tasks(card_status, next_retry_at) WHERE card_status IN ('update_failed', 'expired');

-- 回调日志幂等索引
-- CREATE INDEX idx_callback_idempotent ON wecom_callback_logs(task_id, from_userid, event_key, created_at);