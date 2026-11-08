DROP INDEX IF EXISTS idx_catalogs_project_language;
DROP INDEX IF EXISTS idx_catalogs_owner_template;
CREATE UNIQUE INDEX idx_catalogs_owner_language ON catalogs (project_id, owner_id, language);
