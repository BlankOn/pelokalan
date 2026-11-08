CREATE TABLE users (
    id            BIGSERIAL PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL,
    lang          TEXT NOT NULL DEFAULT 'id',
    last_login_at TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- One-time codes for registration and sign in. Only an HMAC of the code is stored.
CREATE TABLE otp_codes (
    id         BIGSERIAL PRIMARY KEY,
    email      TEXT NOT NULL,
    purpose    TEXT NOT NULL CHECK (purpose IN ('login', 'register')),
    code_hash  TEXT NOT NULL,
    name       TEXT NOT NULL DEFAULT '',
    attempts   INT NOT NULL DEFAULT 0,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_otp_codes_email ON otp_codes (email, created_at DESC);

CREATE TABLE projects (
    id              BIGSERIAL PRIMARY KEY,
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    source_language TEXT NOT NULL DEFAULT 'en',
    owner_id        BIGINT NOT NULL REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE project_members (
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       TEXT NOT NULL CHECK (role IN ('owner', 'manager', 'translator')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (project_id, user_id)
);
CREATE INDEX idx_project_members_user ON project_members (user_id);

CREATE TABLE project_invitations (
    id          BIGSERIAL PRIMARY KEY,
    project_id  BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    email       TEXT NOT NULL,
    role        TEXT NOT NULL CHECK (role IN ('manager', 'translator')),
    invited_by  BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at  TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ,
    declined_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_project_invitations_email ON project_invitations (email);
CREATE UNIQUE INDEX idx_project_invitations_pending
    ON project_invitations (project_id, email)
    WHERE accepted_at IS NULL AND declined_at IS NULL;

-- A catalog is one translation file. The project template (POT) has is_template = true
-- and an empty language.
CREATE TABLE catalogs (
    id           BIGSERIAL PRIMARY KEY,
    project_id   BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    is_template  BOOLEAN NOT NULL DEFAULT FALSE,
    language     TEXT NOT NULL DEFAULT '',
    filename     TEXT NOT NULL,
    header       TEXT NOT NULL DEFAULT '',
    header_comments TEXT NOT NULL DEFAULT '',
    plural_forms TEXT NOT NULL DEFAULT '',
    nplurals     INT NOT NULL DEFAULT 2,
    created_by   BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_catalogs_project_language ON catalogs (project_id, language);

CREATE TABLE entries (
    id                  BIGSERIAL PRIMARY KEY,
    catalog_id          BIGINT NOT NULL REFERENCES catalogs(id) ON DELETE CASCADE,
    position            INT NOT NULL,
    msgctxt             TEXT,
    msgid               TEXT NOT NULL,
    msgid_plural        TEXT NOT NULL DEFAULT '',
    msgstr              JSONB NOT NULL DEFAULT '[""]',
    fuzzy               BOOLEAN NOT NULL DEFAULT FALSE,
    flags               TEXT NOT NULL DEFAULT '',
    translator_comments TEXT NOT NULL DEFAULT '',
    extracted_comments  TEXT NOT NULL DEFAULT '',
    refs                TEXT NOT NULL DEFAULT '',
    previous            TEXT NOT NULL DEFAULT '',
    obsolete            BOOLEAN NOT NULL DEFAULT FALSE,
    version             INT NOT NULL DEFAULT 1,
    updated_by          BIGINT REFERENCES users(id) ON DELETE SET NULL,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_entries_catalog ON entries (catalog_id, position);
CREATE INDEX idx_entries_msgid ON entries (md5(msgid));
