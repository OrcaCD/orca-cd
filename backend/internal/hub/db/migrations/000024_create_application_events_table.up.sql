CREATE TABLE IF NOT EXISTS application_events (
    id             TEXT PRIMARY KEY,
    application_id TEXT NOT NULL,
    request_id     TEXT,
    type           TEXT NOT NULL,
    source         TEXT NOT NULL,
    status         TEXT NOT NULL,
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
