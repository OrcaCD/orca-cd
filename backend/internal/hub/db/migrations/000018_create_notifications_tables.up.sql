CREATE TABLE IF NOT EXISTS notifications (
    id                TEXT     PRIMARY KEY,
    name              TEXT     NOT NULL,
    enabled           BOOLEAN  NOT NULL DEFAULT 1,
    enable_by_default BOOLEAN  NOT NULL DEFAULT 0,
    status            TEXT     NOT NULL,
    type              TEXT     NOT NULL,
    config            TEXT     NOT NULL,
    created_at        DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at        DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS application_notifications (
    application_id  TEXT NOT NULL,
    notification_id TEXT NOT NULL,
    PRIMARY KEY (application_id, notification_id),
    FOREIGN KEY (application_id) REFERENCES applications(id) ON DELETE CASCADE,
    FOREIGN KEY (notification_id) REFERENCES notifications(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_application_notifications_application_id
    ON application_notifications (application_id);

CREATE INDEX IF NOT EXISTS idx_application_notifications_notification_id
    ON application_notifications (notification_id);
