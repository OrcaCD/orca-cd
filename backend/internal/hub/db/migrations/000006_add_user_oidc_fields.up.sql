ALTER TABLE users ADD COLUMN oidc_subject TEXT;
ALTER TABLE users ADD COLUMN oidc_issuer TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_oidc_identity ON users (oidc_issuer, oidc_subject) WHERE oidc_issuer IS NOT NULL AND oidc_subject IS NOT NULL;
