-- Projects are addressed by a unique slug: /projects/gnome-id
ALTER TABLE projects ADD COLUMN slug TEXT;

WITH base AS (
    SELECT id, trim(BOTH '-' FROM substring(regexp_replace(lower(name), '[^a-z0-9]+', '-', 'g') FROM 1 FOR 46)) AS b
    FROM projects
), fixed AS (
    -- a slug needs at least three characters and one letter
    SELECT id, CASE WHEN length(b) < 3 OR b !~ '[a-z]' THEN 'project-' || id ELSE b END AS b FROM base
), numbered AS (
    SELECT id, b, ROW_NUMBER() OVER (PARTITION BY b ORDER BY id) AS n FROM fixed
)
UPDATE projects p SET slug = CASE WHEN n.n = 1 THEN n.b ELSE n.b || '-' || n.n END
FROM numbered n WHERE n.id = p.id;

ALTER TABLE projects ALTER COLUMN slug SET NOT NULL;
CREATE UNIQUE INDEX idx_projects_slug ON projects (slug);
