-- LLM settings of a project (OpenAI-compatible API). The API key is encrypted.
CREATE TABLE project_ai_settings (
    project_id        BIGINT PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    base_url          TEXT NOT NULL DEFAULT 'https://api.openai.com/v1',
    model             TEXT NOT NULL,
    api_key_enc       TEXT NOT NULL,
    api_key_hint      TEXT NOT NULL DEFAULT '',
    price_input       NUMERIC(12, 4) NOT NULL DEFAULT 0, -- USD per 1M input tokens
    price_output      NUMERIC(12, 4) NOT NULL DEFAULT 0, -- USD per 1M output tokens
    monthly_budget    NUMERIC(12, 2) NOT NULL DEFAULT 0, -- USD, 0 = no limit
    updated_by        BIGINT REFERENCES users(id) ON DELETE SET NULL,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- One AI pre-translation run over a catalog
CREATE TABLE ai_jobs (
    id                BIGSERIAL PRIMARY KEY,
    project_id        BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    catalog_id        BIGINT NOT NULL REFERENCES catalogs(id) ON DELETE CASCADE,
    user_id           BIGINT REFERENCES users(id) ON DELETE SET NULL,
    status            TEXT NOT NULL CHECK (status IN ('running', 'done', 'failed', 'cancelled')),
    model             TEXT NOT NULL,
    total             INT NOT NULL DEFAULT 0,
    processed         INT NOT NULL DEFAULT 0,
    from_glossary     INT NOT NULL DEFAULT 0,
    translated        INT NOT NULL DEFAULT 0,
    flagged           INT NOT NULL DEFAULT 0,
    skipped           INT NOT NULL DEFAULT 0,
    prompt_tokens     BIGINT NOT NULL DEFAULT 0,
    completion_tokens BIGINT NOT NULL DEFAULT 0,
    cost              NUMERIC(12, 6) NOT NULL DEFAULT 0,
    estimated_cost    NUMERIC(12, 6) NOT NULL DEFAULT 0,
    cancel_requested  BOOLEAN NOT NULL DEFAULT FALSE,
    error             TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at       TIMESTAMPTZ
);
CREATE INDEX idx_ai_jobs_catalog ON ai_jobs (catalog_id, created_at DESC);
CREATE INDEX idx_ai_jobs_project_month ON ai_jobs (project_id, created_at);
