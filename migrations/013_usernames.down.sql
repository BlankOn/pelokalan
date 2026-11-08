DROP INDEX IF EXISTS idx_catalogs_owner_filename;
ALTER TABLE otp_codes DROP COLUMN IF EXISTS username;
DROP INDEX IF EXISTS idx_users_username;
ALTER TABLE users DROP COLUMN IF EXISTS username;
