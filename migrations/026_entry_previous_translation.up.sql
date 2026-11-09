-- The translation a machine (AI, glossary, memory) replaced, kept until a person saves the
-- entry, so the editor can compare the two side by side
ALTER TABLE entries ADD COLUMN replaced_msgstr JSONB;
