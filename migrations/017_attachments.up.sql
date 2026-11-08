-- Content-addressed file storage (like an S3 bucket): one row per distinct
-- file, keyed by the hex SHA-256 of its content.
CREATE TABLE blobs (
    key          TEXT PRIMARY KEY,
    size         BIGINT NOT NULL,
    data         BYTEA NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Files attached to discussion comments
CREATE TABLE attachments (
    id           BIGSERIAL PRIMARY KEY,
    comment_id   BIGINT NOT NULL REFERENCES comments(id) ON DELETE CASCADE,
    blob_key     TEXT NOT NULL REFERENCES blobs(key),
    filename     TEXT NOT NULL,
    content_type TEXT NOT NULL,
    size         BIGINT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_attachments_comment ON attachments (comment_id);
CREATE INDEX idx_attachments_blob ON attachments (blob_key);
