ALTER TABLE applications ADD COLUMN rolled_back_at DATETIME;
ALTER TABLE applications ADD COLUMN rolled_back_commit TEXT;
ALTER TABLE applications ADD COLUMN auto_sync_paused INTEGER NOT NULL DEFAULT 0;
