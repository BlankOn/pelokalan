-- Where each glossary pair was found, one row per module with its occurrences there (no cap, unlike the display
-- list in glossary_terms.modules): the editor and the AI prefer a module's own translations.
-- module = the glossary module name ("gnome-calculator.main"); module_key = the catalog module (models.ModuleKey:
-- lowercase, before the first dot, "gnome-calculator").
CREATE TABLE glossary_term_modules (
    term_id     BIGINT NOT NULL REFERENCES glossary_terms(id) ON DELETE CASCADE,
    project_id  BIGINT NOT NULL,
    language    TEXT NOT NULL,
    module      TEXT NOT NULL,
    module_key  TEXT NOT NULL,
    occurrences INT NOT NULL,
    PRIMARY KEY (term_id, module)
);
CREATE INDEX idx_glossary_term_modules_key ON glossary_term_modules (project_id, language, module_key);
CREATE INDEX idx_glossary_term_modules_name ON glossary_term_modules (project_id, language, module);

-- existing rows: a pair from one module counts all its occurrences there, else one per module (the next sync of
-- the group stores exact counts)
INSERT INTO glossary_term_modules (term_id, project_id, language, module, module_key, occurrences)
SELECT t.id, t.project_id, t.language, m.name,
       split_part(lower(regexp_replace(m.name, '^.*/', '')), '.', 1),
       CASE WHEN m.n = 1 THEN t.occurrences ELSE 1 END
FROM glossary_terms t,
     LATERAL (SELECT name, COUNT(*) OVER () AS n
              FROM (SELECT DISTINCT unnest(string_to_array(t.modules, E'\x1f')) AS name) d
              WHERE name <> '') m
ON CONFLICT DO NOTHING;
