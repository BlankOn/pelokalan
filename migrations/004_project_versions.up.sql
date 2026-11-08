-- A project keeps going across releases, e.g. name "gnome", version "51".
CREATE TABLE project_versions (
    id         BIGSERIAL PRIMARY KEY,
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    version    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, name, version)
);

ALTER TABLE glossary_imports ADD COLUMN version_id BIGINT REFERENCES project_versions(id) ON DELETE CASCADE;
ALTER TABLE glossary_terms ADD COLUMN version_id BIGINT REFERENCES project_versions(id) ON DELETE CASCADE;

-- Imports made before versions existed get a version named after their file
-- ("gnome-51.ui.id.tar.gz" -> name "gnome", version "51").
INSERT INTO project_versions (project_id, name, version)
SELECT DISTINCT project_id,
       COALESCE(NULLIF(substring(split_part(filename, '.', 1) from '^(.*)-[^-]+$'), ''), split_part(filename, '.', 1)),
       COALESCE(substring(split_part(filename, '.', 1) from '-([^-]+)$'), 'lama')
FROM glossary_imports
ON CONFLICT DO NOTHING;

UPDATE glossary_imports i SET version_id = v.id
FROM project_versions v
WHERE v.project_id = i.project_id
  AND v.name = COALESCE(NULLIF(substring(split_part(i.filename, '.', 1) from '^(.*)-[^-]+$'), ''), split_part(i.filename, '.', 1))
  AND v.version = COALESCE(substring(split_part(i.filename, '.', 1) from '-([^-]+)$'), 'lama');

UPDATE glossary_terms t SET version_id = i.version_id FROM glossary_imports i WHERE i.id = t.import_id;

ALTER TABLE glossary_imports ALTER COLUMN version_id SET NOT NULL;
ALTER TABLE glossary_terms ALTER COLUMN version_id SET NOT NULL;

CREATE INDEX idx_glossary_terms_version ON glossary_terms (project_id, language, version_id, source_norm);
CREATE INDEX idx_glossary_imports_version ON glossary_imports (version_id, language);
