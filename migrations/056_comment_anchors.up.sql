-- a comment about a passage of a translation (a reviewer selected it in the table): the passage, where it started
-- (character offset in that plural form) and which form
ALTER TABLE comments ADD COLUMN anchor_text TEXT NOT NULL DEFAULT '',
                     ADD COLUMN anchor_start INT NOT NULL DEFAULT 0,
                     ADD COLUMN anchor_form INT NOT NULL DEFAULT 0;
