-- Deleting a PO file only marks it: it disappears from lists, memory and counts, but the
-- editor, its history and its discussions stay readable (read-only for everyone).
ALTER TABLE catalogs ADD COLUMN deleted_at TIMESTAMPTZ;
ALTER TABLE catalogs ADD COLUMN deleted_by BIGINT REFERENCES users(id) ON DELETE SET NULL;

-- a deleted file no longer blocks its name or the owner's template slot
DROP INDEX IF EXISTS idx_catalogs_owner_filename;
CREATE UNIQUE INDEX idx_catalogs_owner_filename ON catalogs (project_id, owner_id, filename) WHERE deleted_at IS NULL;
DROP INDEX IF EXISTS idx_catalogs_owner_template;
CREATE UNIQUE INDEX idx_catalogs_owner_template ON catalogs (project_id, owner_id) WHERE is_template AND deleted_at IS NULL;
