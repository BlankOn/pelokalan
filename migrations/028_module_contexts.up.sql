-- Konteks is shared per module of a project (e.g. every member's "tecla.*.po" file shows
-- the same note) and edited together by the members. It does not belong to one file:
-- it survives when files are deleted and comes back when someone takes the module again.
CREATE TABLE module_contexts (
    id         BIGSERIAL PRIMARY KEY,
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    module     TEXT NOT NULL, -- file name before the first dot, lowercase
    note       TEXT NOT NULL DEFAULT '',
    version    BIGINT NOT NULL DEFAULT 1, -- bumped on every save (edit conflicts)
    updated_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, module)
);

-- every save, with the text as saved
CREATE TABLE module_context_edits (
    id         BIGSERIAL PRIMARY KEY,
    context_id BIGINT NOT NULL REFERENCES module_contexts(id) ON DELETE CASCADE,
    user_id    BIGINT REFERENCES users(id) ON DELETE SET NULL,
    note       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_module_context_edits ON module_context_edits (context_id, created_at DESC);

-- Move the per-file notes. Several files of one module: their notes are joined (newest first).
INSERT INTO module_contexts (project_id, module, note, updated_by, updated_at)
SELECT project_id, module,
       string_agg(note, E'\n\n' ORDER BY updated_at DESC),
       (array_agg(updated_by ORDER BY updated_at DESC))[1],
       MAX(updated_at)
FROM (
    SELECT c.project_id, lower(split_part(c.filename, '.', 1)) AS module, x.note, x.updated_by, x.updated_at
    FROM catalog_contexts x JOIN catalogs c ON c.id = x.catalog_id
    WHERE btrim(x.note) <> ''
) s
GROUP BY project_id, module;

INSERT INTO module_context_edits (context_id, user_id, note, created_at)
SELECT id, updated_by, note, updated_at FROM module_contexts;

DROP TABLE catalog_contexts;
