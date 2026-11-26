-- Full integration with a GitHub repository through the Pelokalan GitHub App: a module group synced from GitHub
-- may write approved translations straight to its branch (git_direct), using the app's installation on the
-- repository (git_installation, git_write = may write contents; checked when a manager asks, git_access_error).
ALTER TABLE module_groups
    ADD COLUMN git_direct BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN git_installation BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN git_write BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN git_checked_at TIMESTAMPTZ,
    ADD COLUMN git_access_error TEXT NOT NULL DEFAULT '';

-- A member asks for a review of one module (their file); an owner/manager approves it, which commits the file to
-- the repository, or sends it back with a note. pushed_data (gzip) is the repository file as committed, so the
-- member's strings it holds are never sent again over newer repository translations.
CREATE TABLE module_reviews (
    id           BIGSERIAL PRIMARY KEY,
    project_id   BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    catalog_id   BIGINT NOT NULL REFERENCES catalogs(id) ON DELETE CASCADE,
    group_id     BIGINT REFERENCES module_groups(id) ON DELETE SET NULL,
    module       TEXT NOT NULL,
    repo_path    TEXT NOT NULL,
    status       TEXT NOT NULL, -- requested, rejected, withdrawn, pushed
    requested_by BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    note         TEXT NOT NULL DEFAULT '',
    entries      INTEGER NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    reviewed_by  BIGINT REFERENCES users(id) ON DELETE SET NULL,
    reviewed_at  TIMESTAMPTZ,
    review_note  TEXT NOT NULL DEFAULT '',
    commit_sha   TEXT NOT NULL DEFAULT '',
    commit_url   TEXT NOT NULL DEFAULT '',
    pushed_data  BYTEA,
    error        TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX module_reviews_open ON module_reviews (catalog_id) WHERE status = 'requested';
CREATE INDEX module_reviews_project ON module_reviews (project_id, status, created_at DESC);
CREATE INDEX module_reviews_catalog ON module_reviews (catalog_id, created_at DESC);
