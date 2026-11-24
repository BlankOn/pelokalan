-- Module reservations ("Reservasi modul", Settings → Umum): when a project turns them on, a member holds a
-- module (file name before the first dot) while working on it; other members cannot change their copies
-- of it. Released by the holder, by an owner/manager, when all the holder's changes are upstream
-- ("merged"), or after reservation_days without saves ("expired"; 0 = never). Rows are kept as history.
ALTER TABLE projects ADD COLUMN reservations BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE projects ADD COLUMN reservation_days INT NOT NULL DEFAULT 30;

CREATE TABLE module_reservations (
    id             BIGSERIAL PRIMARY KEY,
    project_id     BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    module         TEXT NOT NULL,
    user_id        BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    reserved_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    reminded_at    TIMESTAMPTZ,                -- the "expires soon" email
    released_at    TIMESTAMPTZ,
    released_by    BIGINT REFERENCES users(id) ON DELETE SET NULL,
    release_reason TEXT NOT NULL DEFAULT ''    -- holder, manager, merged, expired
);
-- one holder per module at a time
CREATE UNIQUE INDEX idx_module_reservations_active ON module_reservations (project_id, module) WHERE released_at IS NULL;
CREATE INDEX idx_module_reservations_user ON module_reservations (user_id) WHERE released_at IS NULL;
