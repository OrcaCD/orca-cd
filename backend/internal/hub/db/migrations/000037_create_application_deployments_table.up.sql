CREATE TABLE IF NOT EXISTS application_deployments (
    id             TEXT PRIMARY KEY,
    application_id TEXT NOT NULL,
    request_id     TEXT,
    status         TEXT NOT NULL,
    "commit"       TEXT NOT NULL,
    commit_message TEXT NOT NULL,
    compose_file   TEXT NOT NULL,
    images         TEXT NOT NULL DEFAULT '[]',
    deployed_at    DATETIME,
    created_at     DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at     DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (application_id) REFERENCES applications(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_application_deployments_request_id
    ON application_deployments(request_id) WHERE request_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_application_deployments_app_deployed
    ON application_deployments(application_id, deployed_at DESC);

-- No foreign keys: SQLite cannot drop a column that is part of one. Pruned
-- deployments clear these references themselves.
ALTER TABLE applications ADD COLUMN current_deployment_id TEXT;
ALTER TABLE application_events ADD COLUMN deployment_id TEXT;
CREATE INDEX IF NOT EXISTS idx_application_events_deployment_id
    ON application_events(deployment_id) WHERE deployment_id IS NOT NULL;
