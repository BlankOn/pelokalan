-- Machine translations approved by a person without changing the text
ALTER TABLE catalog_commits ADD COLUMN reviewed INT NOT NULL DEFAULT 0;
