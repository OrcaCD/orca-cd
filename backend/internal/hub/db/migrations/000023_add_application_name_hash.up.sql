ALTER TABLE applications ADD COLUMN name_hash TEXT NOT NULL DEFAULT '';

-- Partial index excludes legacy rows (empty hash, pre-backfill) so they don't
-- collide on (agent_id, ''). Application names were NOT unique before this
-- migration, so existing rows may share a name on the same agent; the startup
-- backfill (BackfillNameHashes) tolerates such collisions instead of failing.
CREATE UNIQUE INDEX IF NOT EXISTS idx_applications_agent_name_hash
    ON applications (agent_id, name_hash) WHERE name_hash != '';
