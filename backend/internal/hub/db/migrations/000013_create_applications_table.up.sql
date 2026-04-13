CREATE TABLE IF NOT EXISTS applications (
    id             TEXT     PRIMARY KEY,
    name           TEXT     NOT NULL,
    repository_id  TEXT     NOT NULL,
    agent_id       TEXT     NOT NULL,
    sync_status    TEXT     NOT NULL,
    health_status  TEXT     NOT NULL,
    branch         TEXT     NOT NULL,
    commit         TEXT     NOT NULL,
    commit_message TEXT     NOT NULL,
    last_synced_at DATETIME,
    path           TEXT     NOT NULL,
    created_at     DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at     DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (repository_id) REFERENCES repositories(id) ON DELETE CASCADE,
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_applications_repository_id
    ON applications (repository_id);

CREATE INDEX IF NOT EXISTS idx_applications_agent_id
    ON applications (agent_id);