DROP TABLE term_extraction_jobs;
ALTER TABLE module_groups DROP COLUMN ai_terms;
ALTER TABLE module_group_imports DROP COLUMN ai_term_count;
DELETE FROM glossary_terms WHERE origin = 'ai';
ALTER TABLE glossary_terms DROP COLUMN origin;
DROP TABLE term_extractions;
