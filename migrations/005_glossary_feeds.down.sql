DROP INDEX IF EXISTS idx_project_versions_feed;
ALTER TABLE project_versions
    DROP COLUMN IF EXISTS glossary_url,
    DROP COLUMN IF EXISTS glossary_language,
    DROP COLUMN IF EXISTS feed_created_by,
    DROP COLUMN IF EXISTS last_fetched_at,
    DROP COLUMN IF EXISTS last_changed_at,
    DROP COLUMN IF EXISTS last_fetch_error,
    DROP COLUMN IF EXISTS fetch_etag,
    DROP COLUMN IF EXISTS fetch_last_modified,
    DROP COLUMN IF EXISTS fetch_content_hash;
