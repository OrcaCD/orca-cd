ALTER TABLE applications ADD COLUMN image_poll_enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE applications ADD COLUMN image_poll_interval_seconds INTEGER NOT NULL DEFAULT 120;
ALTER TABLE applications ADD COLUMN image_poll_delete_old_images INTEGER NOT NULL DEFAULT 0;
