-- Compose services that image polling skips and that don't count for the
-- health of the application, set with x-orcacd.
ALTER TABLE applications ADD COLUMN image_poll_excluded_services TEXT NOT NULL DEFAULT '[]';
ALTER TABLE applications ADD COLUMN health_ignored_services TEXT NOT NULL DEFAULT '[]';
