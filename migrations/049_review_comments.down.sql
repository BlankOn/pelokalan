DROP INDEX IF EXISTS comments_open_review;
ALTER TABLE comments DROP COLUMN IF EXISTS review_id, DROP COLUMN IF EXISTS resolved_at, DROP COLUMN IF EXISTS resolved_by;
