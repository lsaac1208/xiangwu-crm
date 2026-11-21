-- Keep the original provider intact. The backup key is encrypted inside this
-- private configuration field; no historical business data is rewritten.
ALTER TABLE ai_settings ADD COLUMN fallback_json TEXT NOT NULL DEFAULT '{}';
