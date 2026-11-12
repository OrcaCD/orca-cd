-- Gives a role to the users in a group at an OIDC provider. Group names are only
-- unique within their provider.
CREATE TABLE IF NOT EXISTS role_oidc_groups (
    role_id     TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    group_name  TEXT NOT NULL,
    PRIMARY KEY (role_id, provider_id, group_name),
    FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE,
    FOREIGN KEY (provider_id) REFERENCES oidc_providers(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_role_oidc_groups_provider_group ON role_oidc_groups(provider_id, group_name);

-- The groups of a user at the provider as of their last login, so roles can be
-- updated when the groups of a role change.
ALTER TABLE user_oidc_identities ADD COLUMN group_names TEXT NOT NULL DEFAULT '[]';

-- The claim of the ID token or userinfo response that lists the groups of a user.
-- Empty disables syncing roles for the provider.
ALTER TABLE oidc_providers ADD COLUMN groups_claim TEXT NOT NULL DEFAULT '';
