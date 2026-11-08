-- Shareable links that let anyone who signs in join a project with a fixed role
CREATE TABLE project_invite_links (
    id          BIGSERIAL PRIMARY KEY,
    project_id  BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    token       TEXT NOT NULL UNIQUE,
    role        TEXT NOT NULL CHECK (role IN ('manager', 'translator')),
    created_by  BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at  TIMESTAMPTZ,
    max_uses    INT CHECK (max_uses IS NULL OR max_uses > 0),
    uses        INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_project_invite_links_project ON project_invite_links (project_id);
