-- Public profile extras shown on /users/{username}: a short description and links (JSON object of kind → URL:
-- website, github, x, linkedin, mastodon).
ALTER TABLE users ADD COLUMN bio TEXT NOT NULL DEFAULT '', ADD COLUMN links TEXT NOT NULL DEFAULT '{}';
