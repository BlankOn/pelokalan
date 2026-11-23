-- Badge tiers each person has been awarded (computed badges are compared with these to find new ones,
-- which are emailed). Rows are never removed when a count goes down again, so a tier is announced once.
CREATE TABLE badge_awards (
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    badge      TEXT NOT NULL,
    tier       INT NOT NULL,
    awarded_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    emailed    BOOLEAN NOT NULL DEFAULT FALSE, -- false: recorded silently (the first baseline, or emails off)
    PRIMARY KEY (user_id, badge, tier)
);

-- A person can turn off the emails about new badges (profile page)
ALTER TABLE users ADD COLUMN badge_emails_off BOOLEAN NOT NULL DEFAULT FALSE;
