-- which API the project's AI settings talk to: 'openai' (or a compatible one) or 'anthropic' (Claude)
ALTER TABLE project_ai_settings ADD COLUMN provider TEXT NOT NULL DEFAULT 'openai';
