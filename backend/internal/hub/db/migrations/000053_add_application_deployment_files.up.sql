ALTER TABLE applications ADD COLUMN additional_file_paths TEXT NOT NULL DEFAULT '[]';
ALTER TABLE applications ADD COLUMN additional_files TEXT NOT NULL DEFAULT '';
ALTER TABLE applications ADD COLUMN pending_files_revision TEXT NOT NULL DEFAULT '';
ALTER TABLE application_deployments ADD COLUMN additional_files TEXT NOT NULL DEFAULT '';
