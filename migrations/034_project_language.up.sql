-- The language a project translates into. Uploads, imports and syncs in another language are
-- refused. Empty = not chosen yet (nothing is refused). Existing projects get the language most
-- of their files use, else the one most of their glossary imports use.
ALTER TABLE projects ADD COLUMN language TEXT NOT NULL DEFAULT '';

UPDATE projects p SET language = l.language FROM (
    SELECT DISTINCT ON (project_id) project_id, language
    FROM catalogs WHERE language <> '' AND deleted_at IS NULL
    GROUP BY project_id, language ORDER BY project_id, COUNT(*) DESC
) l WHERE l.project_id = p.id;

UPDATE projects p SET language = l.language FROM (
    SELECT DISTINCT ON (project_id) project_id, language
    FROM glossary_imports WHERE language <> ''
    GROUP BY project_id, language ORDER BY project_id, COUNT(*) DESC
) l WHERE l.project_id = p.id AND p.language = '';
