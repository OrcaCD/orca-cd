-- Deleted hashes cannot be recovered. Downgraded local accounts require recovery.
ALTER TABLE users ADD COLUMN password_hash TEXT;
ALTER TABLE users ADD COLUMN password_change_required INTEGER NOT NULL DEFAULT 0;
