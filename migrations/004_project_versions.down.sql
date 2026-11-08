DROP INDEX IF EXISTS idx_glossary_imports_version;
DROP INDEX IF EXISTS idx_glossary_terms_version;
ALTER TABLE glossary_terms DROP COLUMN IF EXISTS version_id;
ALTER TABLE glossary_imports DROP COLUMN IF EXISTS version_id;
DROP TABLE IF EXISTS project_versions;
