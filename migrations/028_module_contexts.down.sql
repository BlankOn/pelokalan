CREATE TABLE catalog_contexts (
    catalog_id BIGINT PRIMARY KEY REFERENCES catalogs(id) ON DELETE CASCADE,
    note       TEXT NOT NULL,
    updated_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
DROP TABLE IF EXISTS module_context_edits;
DROP TABLE IF EXISTS module_contexts;
