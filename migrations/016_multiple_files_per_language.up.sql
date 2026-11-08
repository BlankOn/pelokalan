-- A member may keep several PO files of the same language in a project (one per
-- module, as in GNOME). File names stay unique per member and project
-- (idx_catalogs_owner_filename); each member still has at most one template.
DROP INDEX IF EXISTS idx_catalogs_owner_language;
CREATE UNIQUE INDEX idx_catalogs_owner_template ON catalogs (project_id, owner_id) WHERE is_template;
CREATE INDEX idx_catalogs_project_language ON catalogs (project_id, language);
