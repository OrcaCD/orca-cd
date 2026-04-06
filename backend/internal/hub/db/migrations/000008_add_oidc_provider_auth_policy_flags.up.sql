ALTER TABLE oidc_providers ADD COLUMN require_verified_email INTEGER NOT NULL DEFAULT 0;
ALTER TABLE oidc_providers ADD COLUMN auto_signup INTEGER NOT NULL DEFAULT 1;
