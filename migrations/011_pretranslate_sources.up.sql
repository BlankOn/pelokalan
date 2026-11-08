-- Pre-translation jobs may use translation memory and run without a model
ALTER TABLE ai_jobs ADD COLUMN from_memory INT NOT NULL DEFAULT 0;
