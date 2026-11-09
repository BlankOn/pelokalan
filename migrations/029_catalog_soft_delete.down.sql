DELETE FROM catalogs WHERE deleted_at IS NOT NULL;
DROP INDEX IF EXISTS idx_catalogs_owner_template;
CREATE UNIQUE INDEX idx_catalogs_owner_template ON catalogs (project_id, owner_id) WHERE is_template;
DROP INDEX IF EXISTS idx_catalogs_owner_filename;
CREATE UNIQUE INDEX idx_catalogs_owner_filename ON catalogs (project_id, owner_id, filename);
ALTER TABLE catalogs DROP COLUMN deleted_by;
ALTER TABLE catalogs DROP COLUMN deleted_at;
