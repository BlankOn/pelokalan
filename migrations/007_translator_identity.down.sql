ALTER TABLE users
    DROP COLUMN IF EXISTS translator_name,
    DROP COLUMN IF EXISTS translator_email,
    DROP COLUMN IF EXISTS language_team;
