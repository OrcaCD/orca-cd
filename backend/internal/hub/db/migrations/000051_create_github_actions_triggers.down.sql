DROP INDEX idx_application_events_trigger_id;
ALTER TABLE application_events DROP COLUMN trigger_id;
DROP TABLE github_actions_trigger_skips;
DROP TABLE github_actions_triggers;
