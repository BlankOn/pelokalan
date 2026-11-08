-- Project wiki: Markdown pages in a tree of at most three levels
CREATE TABLE wiki_pages (
    id          BIGSERIAL PRIMARY KEY,
    project_id  BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    parent_id   BIGINT REFERENCES wiki_pages(id) ON DELETE RESTRICT,
    slug        TEXT NOT NULL,
    title       TEXT NOT NULL,
    body        TEXT NOT NULL DEFAULT '',
    position    INT NOT NULL DEFAULT 0,
    created_by  BIGINT REFERENCES users(id) ON DELETE SET NULL,
    updated_by  BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, slug)
);
CREATE INDEX idx_wiki_pages_parent ON wiki_pages (project_id, parent_id);

-- Files uploaded for wiki pages; the bytes live in the shared blob store
CREATE TABLE wiki_files (
    id           BIGSERIAL PRIMARY KEY,
    project_id   BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    blob_key     TEXT NOT NULL REFERENCES blobs(key),
    filename     TEXT NOT NULL,
    content_type TEXT NOT NULL,
    size         BIGINT NOT NULL,
    uploaded_by  BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_wiki_files_project ON wiki_files (project_id);
CREATE INDEX idx_wiki_files_blob ON wiki_files (blob_key);
