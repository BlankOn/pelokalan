-- Indonesian spelling check. spelling_words: the word list taken once from SPELLING_URL (an extract of
-- KBBI; only the words are stored, no meanings). spelling_extra: words a project accepts in addition
-- (names, terms), added by members from the editor.
CREATE TABLE spelling_words (
    word TEXT PRIMARY KEY
);

CREATE TABLE spelling_extra (
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    word       TEXT NOT NULL,
    added_by   BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (project_id, word)
);
