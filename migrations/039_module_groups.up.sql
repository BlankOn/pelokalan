-- "Kelompok modul": what used to be called glossary versions and glossary imports. A module group
-- (e.g. gnome-51) holds the PO archives imported per language; they feed the glossary and the
-- project files. Renamed throughout, including the tables, columns, keys and indexes.
ALTER TABLE project_versions RENAME TO module_groups;
ALTER TABLE module_groups RENAME COLUMN glossary_url TO sync_url;
ALTER TABLE module_groups RENAME COLUMN glossary_language TO sync_language;
ALTER SEQUENCE project_versions_id_seq RENAME TO module_groups_id_seq;
ALTER TABLE module_groups RENAME CONSTRAINT project_versions_pkey TO module_groups_pkey;
ALTER TABLE module_groups RENAME CONSTRAINT project_versions_project_id_name_version_key TO module_groups_project_id_name_version_key;
ALTER TABLE module_groups RENAME CONSTRAINT project_versions_project_id_fkey TO module_groups_project_id_fkey;
ALTER TABLE module_groups RENAME CONSTRAINT project_versions_feed_created_by_fkey TO module_groups_feed_created_by_fkey;
ALTER INDEX idx_project_versions_feed RENAME TO idx_module_groups_feed;

ALTER TABLE glossary_imports RENAME TO module_group_imports;
ALTER TABLE module_group_imports RENAME COLUMN version_id TO group_id;
ALTER SEQUENCE glossary_imports_id_seq RENAME TO module_group_imports_id_seq;
ALTER TABLE module_group_imports RENAME CONSTRAINT glossary_imports_pkey TO module_group_imports_pkey;
ALTER TABLE module_group_imports RENAME CONSTRAINT glossary_imports_project_id_fkey TO module_group_imports_project_id_fkey;
ALTER TABLE module_group_imports RENAME CONSTRAINT glossary_imports_uploaded_by_fkey TO module_group_imports_uploaded_by_fkey;
ALTER TABLE module_group_imports RENAME CONSTRAINT glossary_imports_version_id_fkey TO module_group_imports_group_id_fkey;
ALTER INDEX idx_glossary_imports_project RENAME TO idx_module_group_imports_project;
ALTER INDEX idx_glossary_imports_version RENAME TO idx_module_group_imports_group;

ALTER TABLE glossary_terms RENAME COLUMN version_id TO group_id;
ALTER TABLE glossary_terms RENAME CONSTRAINT glossary_terms_version_id_fkey TO glossary_terms_group_id_fkey;
ALTER INDEX idx_glossary_terms_version RENAME TO idx_glossary_terms_group;

ALTER TABLE project_files RENAME COLUMN version_id TO group_id;
ALTER TABLE project_files RENAME CONSTRAINT project_files_version_id_fkey TO project_files_group_id_fkey;
ALTER INDEX idx_project_files_version RENAME TO idx_project_files_group;

ALTER TABLE group_stats RENAME COLUMN version_id TO group_id;
ALTER TABLE group_stats RENAME CONSTRAINT group_stats_version_id_fkey TO group_stats_group_id_fkey;
