-- Simple linear version history of a catalog. Each commit stores a full
-- gzip-compressed snapshot of the catalog in PO format.
CREATE TABLE catalog_commits (
    id           BIGSERIAL PRIMARY KEY,
    catalog_id   BIGINT NOT NULL REFERENCES catalogs(id) ON DELETE CASCADE,
    parent_id    BIGINT REFERENCES catalog_commits(id) ON DELETE SET NULL,
    kind         TEXT NOT NULL CHECK (kind IN ('manual', 'auto', 'restore', 'initial')),
    message      TEXT NOT NULL,
    author_id    BIGINT REFERENCES users(id) ON DELETE SET NULL,
    snapshot     BYTEA NOT NULL,
    content_hash TEXT NOT NULL,
    total        INT NOT NULL DEFAULT 0,
    translated   INT NOT NULL DEFAULT 0,
    fuzzy        INT NOT NULL DEFAULT 0,
    added        INT NOT NULL DEFAULT 0,
    changed      INT NOT NULL DEFAULT 0,
    removed      INT NOT NULL DEFAULT 0,
    restored_from BIGINT REFERENCES catalog_commits(id) ON DELETE SET NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_catalog_commits_catalog ON catalog_commits (catalog_id, id DESC);
