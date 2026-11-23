-- Module groups synced from a GitHub repository branch (git_repo "owner/name", git_branch), and the path of each
-- PO file inside that repository, kept on project files and on members' copies so Pelokalan can export the
-- members' changes as a git patch against the repository's own file.
ALTER TABLE module_groups ADD COLUMN git_repo TEXT NOT NULL DEFAULT '', ADD COLUMN git_branch TEXT NOT NULL DEFAULT '';
ALTER TABLE project_files ADD COLUMN repo_path TEXT NOT NULL DEFAULT '';
ALTER TABLE catalogs ADD COLUMN repo_path TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_project_files_repo_path ON project_files (project_id, repo_path) WHERE repo_path <> '';
