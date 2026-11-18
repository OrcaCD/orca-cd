ALTER TABLE users ADD COLUMN local_auth_enabled INTEGER NOT NULL DEFAULT 0;
UPDATE users SET local_auth_enabled = 1 WHERE password_hash IS NOT NULL;
