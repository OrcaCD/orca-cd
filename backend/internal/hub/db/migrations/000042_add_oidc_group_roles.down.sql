DELETE FROM user_roles WHERE source = 'oidc';
ALTER TABLE oidc_providers DROP COLUMN groups_claim;
ALTER TABLE roles DROP COLUMN oidc_groups;
