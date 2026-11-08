DROP INDEX IF EXISTS idx_catalogs_owner;
DROP INDEX IF EXISTS idx_catalogs_owner_language;
CREATE UNIQUE INDEX idx_catalogs_project_language ON catalogs (project_id, language);
ALTER TABLE catalogs DROP COLUMN IF EXISTS owner_id;
