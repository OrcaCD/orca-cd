CREATE TABLE IF NOT EXISTS application_events (
    id             TEXT PRIMARY KEY,
    application_id TEXT NOT NULL,
    request_id     TEXT,
    type           TEXT NOT NULL CHECK (type IN ('deployment', 'commit_sync', 'image_update')),
    source         TEXT NOT NULL CHECK (source IN ('manual', 'application_created', 'repository_polling', 'repository_webhook', 'github_actions', 'image_polling', 'image_webhook')),
    status         TEXT NOT NULL CHECK (status IN ('running', 'succeeded', 'failed', 'no_change')),
    actor_user_id  TEXT,
    actor_name     TEXT,
    commit_hash    TEXT,
    commit_message TEXT,
    error_message  TEXT,
    completed_at   DATETIME,
    created_at     DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at     DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (application_id) REFERENCES applications(id) ON DELETE CASCADE,
    FOREIGN KEY (actor_user_id) REFERENCES users(id) ON DELETE SET NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_application_events_request_id
    ON application_events(request_id) WHERE request_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_application_events_app_created
    ON application_events(application_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_application_events_actor_user_id
    ON application_events(actor_user_id);
