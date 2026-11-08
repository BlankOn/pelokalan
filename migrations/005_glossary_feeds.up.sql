-- A version can have a download URL that is fetched periodically to refresh its glossary.
ALTER TABLE project_versions
    ADD COLUMN glossary_url      TEXT NOT NULL DEFAULT '',
    ADD COLUMN glossary_language TEXT NOT NULL DEFAULT '',
    ADD COLUMN feed_created_by   BIGINT REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN last_fetched_at   TIMESTAMPTZ,
    ADD COLUMN last_changed_at   TIMESTAMPTZ,
    ADD COLUMN last_fetch_error  TEXT NOT NULL DEFAULT '',
    ADD COLUMN fetch_etag        TEXT NOT NULL DEFAULT '',
    ADD COLUMN fetch_last_modified TEXT NOT NULL DEFAULT '',
    ADD COLUMN fetch_content_hash  TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_project_versions_feed ON project_versions (last_fetched_at) WHERE glossary_url <> '';
