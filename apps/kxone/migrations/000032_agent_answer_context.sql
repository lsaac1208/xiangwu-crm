-- Agent-only metadata; no historical CRM business records are rewritten.
ALTER TABLE ai_runs ADD COLUMN error_code TEXT NOT NULL DEFAULT '';
ALTER TABLE ai_runs ADD COLUMN analysis_context_json TEXT NOT NULL DEFAULT '{}';
