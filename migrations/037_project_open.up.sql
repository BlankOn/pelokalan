-- Open projects let any signed-in user join as a translator with the "Gabung" button;
-- private projects (the default) need an email invitation or an invite link.
ALTER TABLE projects ADD COLUMN open BOOLEAN NOT NULL DEFAULT false;
