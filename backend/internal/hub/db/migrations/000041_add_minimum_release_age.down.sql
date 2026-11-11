DROP TABLE IF EXISTS application_image_updates;
DROP TABLE IF EXISTS settings;
ALTER TABLE applications DROP COLUMN release_age_service_overrides;
ALTER TABLE applications DROP COLUMN release_age_allowed_registries;
ALTER TABLE applications DROP COLUMN minimum_release_age_seconds;
