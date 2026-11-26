-- Konteks items that belong to no module (general notes for the project): a title, and a module key starting with
-- "~" that no file name produces.
ALTER TABLE module_contexts ADD COLUMN title TEXT NOT NULL DEFAULT '';
