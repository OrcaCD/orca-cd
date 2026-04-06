CREATE TABLE IF NOT EXISTS oidc_providers (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    issuer_url    TEXT NOT NULL,
    client_id     TEXT NOT NULL,
    client_secret TEXT NOT NULL,
    scopes        TEXT NOT NULL DEFAULT '',
    enabled       INTEGER NOT NULL DEFAULT 1,
    created_at    DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at    DATETIME NOT NULL DEFAULT (datetime('now'))
);
