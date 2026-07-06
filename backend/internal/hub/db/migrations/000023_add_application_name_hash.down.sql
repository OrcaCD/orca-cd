DROP INDEX IF EXISTS idx_applications_agent_name_hash;

ALTER TABLE applications DROP COLUMN name_hash;
