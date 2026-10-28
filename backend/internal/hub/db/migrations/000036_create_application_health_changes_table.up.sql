CREATE TABLE IF NOT EXISTS application_health_changes (
    id             TEXT PRIMARY KEY,
    application_id TEXT NOT NULL,
    status         TEXT NOT NULL,
    reason         TEXT,
    services       TEXT,
    changed_at     DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (application_id) REFERENCES applications(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_application_health_changes_app_changed
    ON application_health_changes(application_id, changed_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_application_health_changes_changed
    ON application_health_changes(changed_at);
