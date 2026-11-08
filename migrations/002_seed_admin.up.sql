-- Default account so a fresh deployment can sign in right away.
-- It still has to be listed in EMAIL_WHITELIST and signs in with a one-time code.
INSERT INTO users (email, name, lang)
VALUES ('admin@blankon.id', 'Admin', 'id')
ON CONFLICT (email) DO NOTHING;
