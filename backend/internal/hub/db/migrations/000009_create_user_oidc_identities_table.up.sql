CREATE TABLE IF NOT EXISTS user_oidc_identities (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL,
    provider_id TEXT,
    issuer      TEXT NOT NULL,
    subject     TEXT NOT NULL,
    created_at  DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at  DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_oidc_identities_issuer_subject
    ON user_oidc_identities (issuer, subject);

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_oidc_identities_user_provider
    ON user_oidc_identities (user_id, provider_id)
    WHERE provider_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_user_oidc_identities_user_id
    ON user_oidc_identities (user_id);

CREATE INDEX IF NOT EXISTS idx_user_oidc_identities_provider_id
    ON user_oidc_identities (provider_id);

INSERT INTO user_oidc_identities (id, user_id, provider_id, issuer, subject, created_at, updated_at)
SELECT users.id || '-oidc-legacy',
       users.id,
       NULL,
       users.oidc_issuer,
       users.oidc_subject,
       users.created_at,
       users.updated_at
FROM users
WHERE users.oidc_issuer IS NOT NULL
  AND users.oidc_subject IS NOT NULL
  AND NOT EXISTS (
      SELECT 1
      FROM user_oidc_identities uoi
      WHERE uoi.issuer = users.oidc_issuer AND uoi.subject = users.oidc_subject
  );
