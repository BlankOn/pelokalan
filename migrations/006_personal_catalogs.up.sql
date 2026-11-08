-- Translation files are a personal workspace: each belongs to one member.
ALTER TABLE catalogs ADD COLUMN owner_id BIGINT REFERENCES users(id) ON DELETE CASCADE;
UPDATE catalogs c SET owner_id = COALESCE(c.created_by, p.owner_id) FROM projects p WHERE p.id = c.project_id;
ALTER TABLE catalogs ALTER COLUMN owner_id SET NOT NULL;

DROP INDEX IF EXISTS idx_catalogs_project_language;
CREATE UNIQUE INDEX idx_catalogs_owner_language ON catalogs (project_id, owner_id, language);
CREATE INDEX idx_catalogs_owner ON catalogs (owner_id);
