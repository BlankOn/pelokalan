-- Progress of each module group (glossary version with project files) over time: the totals
-- of its project files, recorded at every sync (at most once an hour) and after uploads.
CREATE TABLE group_stats (
    id           BIGSERIAL PRIMARY KEY,
    project_id   BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    version_id   BIGINT NOT NULL REFERENCES project_versions(id) ON DELETE CASCADE,
    language     TEXT NOT NULL,
    total        INT NOT NULL,
    translated   INT NOT NULL,
    fuzzy        INT NOT NULL,
    recorded_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_group_stats ON group_stats (version_id, language, recorded_at);
