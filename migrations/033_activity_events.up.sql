-- Append-only log of what people do, for insight and analytics. Rows are written by the
-- services when something happens and never updated. catalog_id/entry_id have no foreign key
-- on purpose: the history stays when files or entries go away.
CREATE TABLE activity_events (
    id            BIGSERIAL PRIMARY KEY,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    kind          TEXT NOT NULL,            -- entry.saved, comment.posted, commit.created, user.registered
    user_id       BIGINT REFERENCES users(id) ON DELETE SET NULL,
    project_id    BIGINT REFERENCES projects(id) ON DELETE CASCADE,
    catalog_id    BIGINT,
    entry_id      BIGINT,
    language      TEXT NOT NULL DEFAULT '',
    -- entry.saved: status before/after (untranslated, fuzzy, translated), the machine origin of
    -- the text before the save (ai, glossary, memory or ''), whether the text changed, and the
    -- number of words in the source string
    status_before TEXT NOT NULL DEFAULT '',
    status_after  TEXT NOT NULL DEFAULT '',
    origin        TEXT NOT NULL DEFAULT '',
    text_changed  BOOLEAN NOT NULL DEFAULT FALSE,
    words         INT NOT NULL DEFAULT 0,
    data          JSONB NOT NULL DEFAULT '{}'
);
CREATE INDEX idx_activity_project ON activity_events (project_id, created_at);
CREATE INDEX idx_activity_user ON activity_events (user_id, created_at);
CREATE INDEX idx_activity_kind ON activity_events (kind, created_at);

-- One row per day, project, user and language with the main counts; handy for ad-hoc SQL.
CREATE VIEW activity_daily AS
SELECT date_trunc('day', created_at) AS day, project_id, user_id, language,
       COUNT(*) FILTER (WHERE kind = 'entry.saved' AND status_before = 'untranslated' AND status_after <> 'untranslated') AS translated,
       COALESCE(SUM(words) FILTER (WHERE kind = 'entry.saved' AND status_before = 'untranslated' AND status_after <> 'untranslated'), 0) AS translated_words,
       COUNT(*) FILTER (WHERE kind = 'entry.saved' AND status_before = 'fuzzy' AND status_after = 'translated') AS reviewed,
       COUNT(*) FILTER (WHERE kind = 'entry.saved' AND status_before = 'translated' AND text_changed) AS edited,
       COUNT(*) FILTER (WHERE kind = 'entry.saved' AND origin <> '' AND NOT text_changed) AS drafts_approved,
       COUNT(*) FILTER (WHERE kind = 'entry.saved' AND origin <> '' AND text_changed) AS drafts_fixed,
       COUNT(*) FILTER (WHERE kind = 'comment.posted') AS comments,
       COUNT(*) FILTER (WHERE kind = 'commit.created') AS commits
FROM activity_events
GROUP BY 1, 2, 3, 4;
