ALTER TABLE notifications RENAME COLUMN enable_by_default TO all_applications;
ALTER TABLE notifications ADD COLUMN events TEXT NOT NULL DEFAULT '[]';

-- Existing notifications keep receiving every event that was sent before events became selectable.
UPDATE notifications SET events = '["application.deployment.succeeded","application.deployment.failed","application.image_update.succeeded","application.image_update.failed","application.sync.failed"]';
