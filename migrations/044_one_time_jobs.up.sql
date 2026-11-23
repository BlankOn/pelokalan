-- Jobs that run once on start-up (e.g. a one-off backfill); a row means it has run
CREATE TABLE one_time_jobs (
    name    TEXT PRIMARY KEY,
    done_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
