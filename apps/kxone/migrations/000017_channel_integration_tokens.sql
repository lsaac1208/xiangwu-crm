-- Store per-account Xiaohongshu webhook tokens encrypted by the application.

-- +kxone dialect sqlite
ALTER TABLE channel_integrations ADD COLUMN token_ciphertext text;
UPDATE channel_integrations SET secret_configured = 0 WHERE platform = 'xiaohongshu';

-- +kxone dialect mysql
ALTER TABLE channel_integrations ADD COLUMN token_ciphertext text;
UPDATE channel_integrations SET secret_configured = false WHERE platform = 'xiaohongshu';
