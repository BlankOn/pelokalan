-- Module groups synced from a Translation Project team page (tp_team = the team code, "id"), and the team's
-- domains as last read from that page, shared by every project: the current version, counts, last submission,
-- assigned translator, whether a disclaimer is needed, and the downloaded file (gzip) so an unchanged domain is
-- not downloaded again.
ALTER TABLE module_groups ADD COLUMN tp_team TEXT NOT NULL DEFAULT '';
CREATE TABLE tp_domains (
    team        TEXT NOT NULL,
    domain      TEXT NOT NULL,
    version     TEXT NOT NULL,
    file_url    TEXT NOT NULL,
    template    BOOLEAN NOT NULL DEFAULT false,
    translated  INTEGER NOT NULL DEFAULT 0,
    total       INTEGER NOT NULL DEFAULT 0,
    updated     DATE,
    translator  TEXT NOT NULL DEFAULT '',
    restricted  BOOLEAN NOT NULL DEFAULT false,
    fingerprint TEXT NOT NULL DEFAULT '',
    data        BYTEA,
    checked_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (team, domain)
);
