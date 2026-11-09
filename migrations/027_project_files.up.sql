-- PO files contained in glossary sources (e.g. a GNOME release tarball), kept so members
-- can take a copy to translate or download the original. They belong to their import.
CREATE TABLE project_files (
    id          BIGSERIAL PRIMARY KEY,
    import_id   BIGINT NOT NULL REFERENCES glossary_imports(id) ON DELETE CASCADE,
    project_id  BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    version_id  BIGINT NOT NULL REFERENCES project_versions(id) ON DELETE CASCADE,
    filename    TEXT NOT NULL,
    language    TEXT NOT NULL,
    total       INT NOT NULL DEFAULT 0,
    translated  INT NOT NULL DEFAULT 0,
    fuzzy       INT NOT NULL DEFAULT 0,
    size        BIGINT NOT NULL DEFAULT 0,
    data        BYTEA NOT NULL, -- gzip-compressed PO file
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_project_files_project ON project_files (project_id);
CREATE INDEX idx_project_files_version ON project_files (version_id);
