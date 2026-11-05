-- Users get the roles whose groups they are a member of at their OIDC provider.
ALTER TABLE roles ADD COLUMN oidc_groups TEXT NOT NULL DEFAULT '[]';
-- The claim of the ID token or userinfo response that lists the groups of a user.
-- Empty disables syncing roles for the provider.
ALTER TABLE oidc_providers ADD COLUMN groups_claim TEXT NOT NULL DEFAULT 'groups';
