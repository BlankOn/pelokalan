-- Indonesian thesaurus (synonyms/antonyms), downloaded once from THESAURUS_URL
CREATE TABLE thesaurus (
    word     TEXT PRIMARY KEY,
    tag      TEXT NOT NULL DEFAULT '',
    synonyms JSONB NOT NULL DEFAULT '[]',
    antonyms JSONB NOT NULL DEFAULT '[]'
);
