-- Editor settings per account: checks the user switched off (comma separated issue codes,
-- plus "untranslated"/"fuzzy" for what Validasi shows). Empty = everything on, so new checks
-- are on by default.
ALTER TABLE users ADD COLUMN editor_checks_off TEXT NOT NULL DEFAULT '';
