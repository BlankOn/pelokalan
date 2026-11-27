-- Glossary terms a language model found in long strings (checked against both texts). The results are kept per
-- string (source + translation), shared by every project, so each string is sent once.
CREATE TABLE term_extractions (
    language    TEXT NOT NULL,
    string_hash TEXT NOT NULL, -- SHA-256 hex of source || 0x00 || translation
    pairs       JSONB NOT NULL, -- [{"s": "removed", "t": "dihapus"}, ...]
    model       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (language, string_hash)
);

-- '' = a short string of the source; 'ai' = found by extraction from a long string
ALTER TABLE glossary_terms ADD COLUMN origin TEXT NOT NULL DEFAULT '';
ALTER TABLE module_group_imports ADD COLUMN ai_term_count INT NOT NULL DEFAULT 0;
-- extraction was started for the group: strings that are new after a sync are extracted too
ALTER TABLE module_groups ADD COLUMN ai_terms BOOLEAN NOT NULL DEFAULT FALSE;

-- extraction runs (their cost counts toward the project's monthly AI budget)
CREATE TABLE term_extraction_jobs (
    id                BIGSERIAL PRIMARY KEY,
    project_id        BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    group_id          BIGINT REFERENCES module_groups(id) ON DELETE SET NULL,
    user_id           BIGINT REFERENCES users(id) ON DELETE SET NULL, -- NULL = after a sync
    status            TEXT NOT NULL CHECK (status IN ('running', 'done', 'failed', 'cancelled')),
    model             TEXT NOT NULL,
    total             INT NOT NULL DEFAULT 0,
    processed         INT NOT NULL DEFAULT 0,
    pairs             INT NOT NULL DEFAULT 0,
    prompt_tokens     BIGINT NOT NULL DEFAULT 0,
    completion_tokens BIGINT NOT NULL DEFAULT 0,
    cost              NUMERIC(12, 6) NOT NULL DEFAULT 0,
    error             TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at       TIMESTAMPTZ
);
CREATE INDEX idx_term_extraction_jobs_project ON term_extraction_jobs (project_id, created_at DESC);
