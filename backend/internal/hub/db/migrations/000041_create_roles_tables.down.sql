ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user';

UPDATE users SET role = 'admin' WHERE id IN (
    SELECT user_roles.user_id
    FROM user_roles
    JOIN roles ON roles.id = user_roles.role_id
    WHERE roles.built_in = 'admin'
);

DROP INDEX IF EXISTS idx_user_roles_role_id;
DROP TABLE IF EXISTS user_roles;
DROP INDEX IF EXISTS idx_role_permissions_resource_id;
DROP INDEX IF EXISTS idx_role_permissions_unique;
DROP TABLE IF EXISTS role_permissions;
DROP INDEX IF EXISTS idx_roles_name;
DROP INDEX IF EXISTS idx_roles_built_in;
DROP TABLE IF EXISTS roles;
