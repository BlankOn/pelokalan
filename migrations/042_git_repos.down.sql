DROP INDEX idx_project_files_repo_path;
ALTER TABLE catalogs DROP COLUMN repo_path;
ALTER TABLE project_files DROP COLUMN repo_path;
ALTER TABLE module_groups DROP COLUMN git_branch, DROP COLUMN git_repo;
