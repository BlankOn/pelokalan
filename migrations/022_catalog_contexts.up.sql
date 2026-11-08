-- Konteks: a free note about a PO file (what it is, where it is used, team decisions…),
-- kept in Pelokalan only (not exported). Editable by the file owner, readable by all.
CREATE TABLE catalog_contexts (
    catalog_id BIGINT PRIMARY KEY REFERENCES catalogs(id) ON DELETE CASCADE,
    note       TEXT NOT NULL,
    updated_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
