DROP TABLE IF EXISTS comment_mentions;
DROP TABLE IF EXISTS comments;
DROP TABLE IF EXISTS discussions;
DROP INDEX IF EXISTS idx_glossary_terms_hash;
ALTER TABLE glossary_terms DROP COLUMN IF EXISTS source_hash;
