-- Keep the roles users got from groups, so the hub doesn't lose its admins
INSERT OR IGNORE INTO user_roles (user_id, role_id, source, created_at)
    SELECT user_id, role_id, 'manual', created_at FROM user_roles WHERE source = 'oidc';
DELETE FROM user_roles WHERE source = 'oidc';
ALTER TABLE oidc_providers DROP COLUMN groups_claim;
ALTER TABLE user_oidc_identities DROP COLUMN group_names;
DROP TABLE IF EXISTS role_oidc_groups;
