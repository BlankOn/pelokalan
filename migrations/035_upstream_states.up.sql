-- State of translations on the GNOME translation site, one row per release, language and file
-- name stem ("gnome-maps.gnome-51"). Shared by every project; filled gradually by the background
-- sync because the site's API is rate limited. listed = false: the release has no such file.
CREATE TABLE upstream_states (
    release      TEXT NOT NULL,
    language     TEXT NOT NULL,
    stem         TEXT NOT NULL,
    listed       BOOLEAN NOT NULL DEFAULT TRUE,
    module       TEXT NOT NULL DEFAULT '',
    branch       TEXT NOT NULL DEFAULT '',
    domain       TEXT NOT NULL DEFAULT '',
    state        TEXT NOT NULL DEFAULT '',
    translated   INT NOT NULL DEFAULT 0,
    fuzzy        INT NOT NULL DEFAULT 0,
    untranslated INT NOT NULL DEFAULT 0,
    po_file      TEXT NOT NULL DEFAULT '',
    checked_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (release, language, stem)
);
