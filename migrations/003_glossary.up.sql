-- A glossary import is one uploaded archive (or PO file) of existing translations.
CREATE TABLE glossary_imports (
    id          BIGSERIAL PRIMARY KEY,
    project_id  BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    filename    TEXT NOT NULL,
    language    TEXT NOT NULL,
    file_count  INT NOT NULL DEFAULT 0,
    entry_count INT NOT NULL DEFAULT 0,
    term_count  INT NOT NULL DEFAULT 0,
    uploaded_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_glossary_imports_project ON glossary_imports (project_id);

-- One row per distinct (source, translation) pair of an import.
CREATE TABLE glossary_terms (
    id               BIGSERIAL PRIMARY KEY,
    import_id        BIGINT NOT NULL REFERENCES glossary_imports(id) ON DELETE CASCADE,
    project_id       BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    language         TEXT NOT NULL,
    source           TEXT NOT NULL,
    source_norm      TEXT NOT NULL,
    translation      TEXT NOT NULL,
    translation_norm TEXT NOT NULL,
    word_count       INT NOT NULL,
    occurrences      INT NOT NULL,
    modules          TEXT NOT NULL DEFAULT '',
    contexts         TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_glossary_terms_lookup ON glossary_terms (project_id, language, source_norm);
CREATE INDEX idx_glossary_terms_import ON glossary_terms (import_id);
