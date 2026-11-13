-- The file's page on the GNOME translation site (…/vertimus/<module>/<branch>/<domain>/<lang>/), entered when a
-- file is added or updated by hand, so its upstream state is fetched even without a matching project file.
ALTER TABLE catalogs ADD COLUMN upstream_page TEXT NOT NULL DEFAULT '';
