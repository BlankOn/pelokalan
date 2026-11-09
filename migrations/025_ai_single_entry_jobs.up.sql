-- Single-entry AI translations (the editor's AI button) are recorded as jobs so their cost
-- counts toward the budget, but they are not shown as the file's pre-translation job
ALTER TABLE ai_jobs ADD COLUMN single BOOLEAN NOT NULL DEFAULT FALSE;
