-- Personal access tokens for the REST API. Only a hash of the token is stored;
-- the plain token is shown once when it is created.
CREATE TABLE IF NOT EXISTS api_tokens (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL,
    name         TEXT NOT NULL,
    token_hash   TEXT NOT NULL,
    prefix       TEXT NOT NULL,
    scopes       TEXT NOT NULL DEFAULT '[]',
    expires_at   DATETIME NOT NULL,
    last_used_at DATETIME,
    created_at   DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at   DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_api_tokens_token_hash ON api_tokens(token_hash);
CREATE UNIQUE INDEX IF NOT EXISTS idx_api_tokens_user_name ON api_tokens(user_id, name COLLATE NOCASE);
