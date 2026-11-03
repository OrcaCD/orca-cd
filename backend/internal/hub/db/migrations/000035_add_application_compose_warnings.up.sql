ALTER TABLE applications ADD COLUMN compose_file_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE applications ADD COLUMN warnings TEXT NOT NULL DEFAULT '[]';
