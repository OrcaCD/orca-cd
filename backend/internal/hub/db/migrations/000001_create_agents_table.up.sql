CREATE TABLE IF NOT EXISTS agents (
    id         TEXT    PRIMARY KEY,
    name       TEXT    NOT NULL,
    secret     TEXT    NOT NULL,
    status     INTEGER NOT NULL DEFAULT 0,
    last_seen  DATETIME,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
