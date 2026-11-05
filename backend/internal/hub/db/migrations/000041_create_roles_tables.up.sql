CREATE TABLE IF NOT EXISTS roles (
    id          TEXT PRIMARY KEY,
    built_in    TEXT,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_roles_built_in ON roles(built_in) WHERE built_in IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_roles_name ON roles(name COLLATE NOCASE);

-- A NULL resource_id grants the action on all resources of the type.
CREATE TABLE IF NOT EXISTS role_permissions (
    id          TEXT PRIMARY KEY,
    role_id     TEXT NOT NULL,
    resource    TEXT NOT NULL,
    action      TEXT NOT NULL,
    resource_id TEXT,
    created_at  DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at  DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_role_permissions_unique
    ON role_permissions(role_id, resource, action, IFNULL(resource_id, ''));
CREATE INDEX IF NOT EXISTS idx_role_permissions_resource_id
    ON role_permissions(resource, resource_id) WHERE resource_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS user_roles (
    user_id    TEXT NOT NULL,
    role_id    TEXT NOT NULL,
    source     TEXT NOT NULL DEFAULT 'manual',
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (user_id, role_id, source),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_user_roles_role_id ON user_roles(role_id);

INSERT INTO roles (id, built_in, name, description) VALUES
    ('00000000-0000-7000-8000-000000000001', 'admin', 'Admin', 'Full access, including the admin pages'),
    ('00000000-0000-7000-8000-000000000002', 'user', 'User', 'Default role for new users');

-- The user role keeps the access every user had before roles existed.
INSERT INTO role_permissions (id, role_id, resource, action)
SELECT lower(hex(randomblob(16))), '00000000-0000-7000-8000-000000000002', resource, action
FROM (
    SELECT 'application' AS resource, 'read' AS action
    UNION ALL SELECT 'application', 'create'
    UNION ALL SELECT 'application', 'update'
    UNION ALL SELECT 'application', 'delete'
    UNION ALL SELECT 'application', 'deploy'
    UNION ALL SELECT 'application', 'pull_images'
    UNION ALL SELECT 'application', 'rollback'
    UNION ALL SELECT 'repository', 'read'
    UNION ALL SELECT 'repository', 'create'
    UNION ALL SELECT 'repository', 'update'
    UNION ALL SELECT 'repository', 'delete'
    UNION ALL SELECT 'repository', 'sync'
    UNION ALL SELECT 'agent', 'read'
    UNION ALL SELECT 'agent', 'create'
    UNION ALL SELECT 'agent', 'update'
    UNION ALL SELECT 'agent', 'delete'
    UNION ALL SELECT 'notification', 'read'
    UNION ALL SELECT 'notification', 'create'
    UNION ALL SELECT 'notification', 'update'
    UNION ALL SELECT 'notification', 'delete'
);

INSERT INTO user_roles (user_id, role_id, source)
SELECT users.id, roles.id, 'manual'
FROM users
JOIN roles ON roles.built_in = CASE WHEN users.role = 'admin' THEN 'admin' ELSE 'user' END;

ALTER TABLE users DROP COLUMN role;
