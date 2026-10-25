ALTER TABLE repositories ADD COLUMN require_signed_commits INTEGER NOT NULL DEFAULT 0;
ALTER TABLE repositories ADD COLUMN commit_signing_keys TEXT NOT NULL DEFAULT '';
