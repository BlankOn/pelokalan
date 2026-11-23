-- Strings a member translated in Pelokalan that later appeared, word for word, in the upstream file
-- (a GNOME release archive or a GitHub branch) at a sync: their work was accepted upstream. One row per
-- member, file and string; written once (the first sync that shows it), never updated.
CREATE TABLE upstream_landings (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    catalog_id BIGINT NOT NULL,          -- no foreign key: the record stays when the file goes
    group_id   BIGINT,                   -- the module group whose sync showed it
    entry_key  TEXT NOT NULL,            -- Entry.KeyHash (context + source)
    module     TEXT NOT NULL DEFAULT '',
    filename   TEXT NOT NULL DEFAULT '',
    words      INT NOT NULL DEFAULT 0,   -- words in the source string
    landed_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, catalog_id, entry_key)
);
CREATE INDEX idx_upstream_landings_user ON upstream_landings (user_id, landed_at);
