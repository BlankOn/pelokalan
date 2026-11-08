-- Stable id of a glossary string: sha256 of its normalized source text
ALTER TABLE glossary_terms ADD COLUMN source_hash TEXT NOT NULL DEFAULT '';
UPDATE glossary_terms SET source_hash = encode(sha256(convert_to(source_norm, 'UTF8')), 'hex');
CREATE INDEX idx_glossary_terms_hash ON glossary_terms (project_id, source_hash);

-- A discussion is attached to any subject of a project, identified by a type
-- and a key (e.g. type 'glossary_term', key = the term hash).
CREATE TABLE discussions (
    id               BIGSERIAL PRIMARY KEY,
    project_id       BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    subject_type     TEXT NOT NULL,
    subject_key      TEXT NOT NULL,
    title            TEXT NOT NULL,
    created_by       BIGINT REFERENCES users(id) ON DELETE SET NULL,
    comment_count    INT NOT NULL DEFAULT 0,
    last_activity_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, subject_type, subject_key)
);
CREATE INDEX idx_discussions_activity ON discussions (project_id, last_activity_at DESC);

CREATE TABLE comments (
    id            BIGSERIAL PRIMARY KEY,
    discussion_id BIGINT NOT NULL REFERENCES discussions(id) ON DELETE CASCADE,
    author_id     BIGINT REFERENCES users(id) ON DELETE SET NULL,
    body          TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    edited_at     TIMESTAMPTZ,
    deleted_at    TIMESTAMPTZ,
    deleted_by    BIGINT REFERENCES users(id) ON DELETE SET NULL
);
CREATE INDEX idx_comments_discussion ON comments (discussion_id, id);

CREATE TABLE comment_mentions (
    comment_id BIGINT NOT NULL REFERENCES comments(id) ON DELETE CASCADE,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (comment_id, user_id)
);
CREATE INDEX idx_comment_mentions_user ON comment_mentions (user_id);
