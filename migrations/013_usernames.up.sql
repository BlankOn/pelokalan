-- Every user has a unique username, used in URLs such as /projects/1/<username>/po/<file>
ALTER TABLE users ADD COLUMN username TEXT;

-- Existing users get one derived from their email address ("ani.s@x.id" -> "ani.s"),
-- with a number appended when it is taken.
WITH base AS (
    SELECT id,
           COALESCE(NULLIF(trim(BOTH '._-' FROM substring(regexp_replace(lower(split_part(email, '@', 1)), '[^a-z0-9._-]', '', 'g') FROM 1 FOR 26)), ''), 'user') AS b
    FROM users
), padded AS (
    SELECT id, CASE WHEN length(b) < 3 THEN b || repeat('0', 3 - length(b)) ELSE b END AS b FROM base
), numbered AS (
    SELECT id, b, ROW_NUMBER() OVER (PARTITION BY b ORDER BY id) AS n FROM padded
)
UPDATE users u SET username = CASE WHEN n.n = 1 THEN n.b ELSE n.b || n.n END
FROM numbered n WHERE n.id = u.id;

ALTER TABLE users ALTER COLUMN username SET NOT NULL;
CREATE UNIQUE INDEX idx_users_username ON users (username);

-- Pending registrations carry the chosen username
ALTER TABLE otp_codes ADD COLUMN username TEXT NOT NULL DEFAULT '';

-- File names are unique within a user's workspace in a project, so a URL names one file
WITH dup AS (
    SELECT id, ROW_NUMBER() OVER (PARTITION BY project_id, owner_id, filename ORDER BY id) AS n
    FROM catalogs
)
UPDATE catalogs c SET filename = regexp_replace(c.filename, '(\.pot?)$', '') || '-' || COALESCE(NULLIF(c.language, ''), 'template') || '-' || c.id ||
       COALESCE(substring(c.filename from '(\.pot?)$'), '')
FROM dup WHERE dup.id = c.id AND dup.n > 1;
CREATE UNIQUE INDEX idx_catalogs_owner_filename ON catalogs (project_id, owner_id, filename);
