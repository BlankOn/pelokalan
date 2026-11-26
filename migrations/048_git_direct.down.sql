DROP TABLE IF EXISTS module_reviews;
ALTER TABLE module_groups DROP COLUMN IF EXISTS git_direct, DROP COLUMN IF EXISTS git_installation,
    DROP COLUMN IF EXISTS git_write, DROP COLUMN IF EXISTS git_checked_at, DROP COLUMN IF EXISTS git_access_error;
