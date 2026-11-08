-- Project-wide guidance sent with every AI pre-translation request of the project
ALTER TABLE project_ai_settings ADD COLUMN extra_context TEXT NOT NULL DEFAULT '';
