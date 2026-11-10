-- Per member and project: which discussion emails they want. Everything is on unless listed
-- in "off" (so new kinds start on); "muted" turns off every email from the project.
CREATE TABLE notification_settings (
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    muted      BOOLEAN NOT NULL DEFAULT FALSE,
    off        TEXT[] NOT NULL DEFAULT '{}',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, project_id)
);
