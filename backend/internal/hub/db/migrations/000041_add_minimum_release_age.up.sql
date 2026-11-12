-- NULL uses the global default of the settings table.
ALTER TABLE applications ADD COLUMN minimum_release_age_seconds INTEGER;
ALTER TABLE applications ADD COLUMN release_age_allowed_registries TEXT NOT NULL DEFAULT '[]';
ALTER TABLE applications ADD COLUMN release_age_service_overrides TEXT NOT NULL DEFAULT '{}';

-- Global settings, a single row.
CREATE TABLE IF NOT EXISTS settings (
    id                             INTEGER PRIMARY KEY CHECK (id = 1),
    minimum_release_age_seconds    INTEGER NOT NULL DEFAULT 0,
    release_age_allowed_registries TEXT NOT NULL DEFAULT '[]'
);
INSERT OR IGNORE INTO settings (id) VALUES (1);

-- The update state of the image of every compose service, from the last image check.
CREATE TABLE IF NOT EXISTS application_image_updates (
    application_id         TEXT NOT NULL,
    service                TEXT NOT NULL,
    image                  TEXT NOT NULL,
    status                 TEXT NOT NULL,
    current_digest         TEXT NOT NULL DEFAULT '',
    current_version        TEXT NOT NULL DEFAULT '',
    new_digest             TEXT NOT NULL DEFAULT '',
    new_version            TEXT NOT NULL DEFAULT '',
    pushed_at              DATETIME,
    first_seen             INTEGER NOT NULL DEFAULT 0,
    eligible_at            DATETIME,
    minimum_age_seconds    INTEGER NOT NULL DEFAULT 0,
    minimum_age_from_label INTEGER NOT NULL DEFAULT 0,
    reason                 TEXT NOT NULL DEFAULT '',
    checked_at             DATETIME NOT NULL,
    PRIMARY KEY (application_id, service),
    FOREIGN KEY (application_id) REFERENCES applications(id) ON DELETE CASCADE
);
