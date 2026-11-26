DELETE FROM module_contexts WHERE module LIKE '~%'; ALTER TABLE module_contexts DROP COLUMN IF EXISTS title;
