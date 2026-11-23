CREATE TABLE github_actions_triggers (
    id TEXT PRIMARY KEY,
    repository_id TEXT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    repository TEXT NOT NULL,
    run_id TEXT NOT NULL,
    sync_repo BOOLEAN NOT NULL,
    pull_images BOOLEAN NOT NULL,
    application_ids TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

CREATE INDEX idx_github_actions_triggers_created_at ON github_actions_triggers(created_at);

CREATE TABLE github_actions_trigger_skips (
    trigger_id TEXT NOT NULL REFERENCES github_actions_triggers(id) ON DELETE CASCADE,
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    type TEXT NOT NULL,
    reason TEXT NOT NULL,
    PRIMARY KEY (trigger_id, application_id, type)
);

CREATE INDEX idx_github_actions_trigger_skips_application_id ON github_actions_trigger_skips(application_id);

ALTER TABLE application_events ADD COLUMN trigger_id TEXT;
CREATE INDEX idx_application_events_trigger_id ON application_events(trigger_id) WHERE trigger_id IS NOT NULL;
