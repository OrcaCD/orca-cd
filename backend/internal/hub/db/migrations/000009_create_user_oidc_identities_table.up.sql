CREATE TABLE IF NOT EXISTS user_oidc_identities (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    subject     TEXT NOT NULL,
    created_at  DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at  DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_oidc_identities_provider_subject
    ON user_oidc_identities (provider_id, subject);

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_oidc_identities_user_provider
    ON user_oidc_identities (user_id, provider_id);

CREATE INDEX IF NOT EXISTS idx_user_oidc_identities_user_id
    ON user_oidc_identities (user_id);

CREATE INDEX IF NOT EXISTS idx_user_oidc_identities_provider_id
    ON user_oidc_identities (provider_id);
