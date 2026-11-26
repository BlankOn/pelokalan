-- Project setting: files go to a repository Pelokalan writes to only through review (an owner/manager approves a
-- request); the direct "Kirim ke repositori" button is then gone and refused.
ALTER TABLE projects ADD COLUMN review_required BOOLEAN NOT NULL DEFAULT false;
