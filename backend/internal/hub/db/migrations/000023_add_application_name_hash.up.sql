ALTER TABLE applications ADD COLUMN name_hash TEXT NOT NULL DEFAULT '';

-- Partial index excludes legacy rows (empty hash, pre-backfill) so they don't
-- collide on (agent_id, ''). Names were globally unique under the old rule, so
-- the per-agent backfill cannot produce a collision.
CREATE UNIQUE INDEX IF NOT EXISTS idx_applications_agent_name_hash
    ON applications (agent_id, name_hash) WHERE name_hash != '';
