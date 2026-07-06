CREATE TABLE IF NOT EXISTS repositories (
    id               TEXT     PRIMARY KEY,
    name             TEXT     NOT NULL,
    url              TEXT     NOT NULL,
    provider         TEXT     NOT NULL,
    auth_method      TEXT     NOT NULL,
    auth_user        TEXT,
    auth_token       TEXT,
    sync_type        TEXT     NOT NULL,
    sync_status      TEXT     NOT NULL DEFAULT 'unknown',
    last_sync_error  TEXT,
    polling_interval INTEGER,
    webhook_secret   TEXT,
    last_synced_at   DATETIME,
    created_by       TEXT     NOT NULL,
    created_at       DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at       DATETIME NOT NULL DEFAULT (datetime('now'))
);
