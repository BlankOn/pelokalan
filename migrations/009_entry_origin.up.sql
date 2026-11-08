-- Where the current translation came from when it was not typed by a person:
-- 'ai' (language model), 'glossary' (applied from the glossary), 'memory'
-- (pre-translation from translation memory). Cleared when a person saves the entry.
ALTER TABLE entries ADD COLUMN origin TEXT NOT NULL DEFAULT ''
    CHECK (origin IN ('', 'ai', 'glossary', 'memory'));
