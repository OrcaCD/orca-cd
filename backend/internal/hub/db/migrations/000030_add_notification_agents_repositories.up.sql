ALTER TABLE notifications ADD COLUMN all_agents BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE notifications ADD COLUMN all_repositories BOOLEAN NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS agent_notifications (
    agent_id        TEXT NOT NULL,
    notification_id TEXT NOT NULL,
    PRIMARY KEY (agent_id, notification_id),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE,
    FOREIGN KEY (notification_id) REFERENCES notifications(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_agent_notifications_agent_id
    ON agent_notifications (agent_id);

CREATE INDEX IF NOT EXISTS idx_agent_notifications_notification_id
    ON agent_notifications (notification_id);

CREATE TABLE IF NOT EXISTS repository_notifications (
    repository_id   TEXT NOT NULL,
    notification_id TEXT NOT NULL,
    PRIMARY KEY (repository_id, notification_id),
    FOREIGN KEY (repository_id) REFERENCES repositories(id) ON DELETE CASCADE,
    FOREIGN KEY (notification_id) REFERENCES notifications(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_repository_notifications_repository_id
    ON repository_notifications (repository_id);

CREATE INDEX IF NOT EXISTS idx_repository_notifications_notification_id
    ON repository_notifications (notification_id);
