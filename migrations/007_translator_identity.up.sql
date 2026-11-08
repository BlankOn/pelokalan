-- How a user is credited in PO headers (Last-Translator, contributor lines, Language-Team)
ALTER TABLE users
    ADD COLUMN translator_name  TEXT NOT NULL DEFAULT '',
    ADD COLUMN translator_email TEXT NOT NULL DEFAULT '',
    ADD COLUMN language_team    TEXT NOT NULL DEFAULT '';
