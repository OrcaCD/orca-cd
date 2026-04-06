ALTER TABLE users ADD COLUMN auth_provider TEXT NOT NULL DEFAULT 'local';
UPDATE users SET auth_provider = 'oidc' WHERE oidc_issuer IS NOT NULL AND oidc_subject IS NOT NULL;
