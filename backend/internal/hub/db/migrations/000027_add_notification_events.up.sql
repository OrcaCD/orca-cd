ALTER TABLE notifications RENAME COLUMN enable_by_default TO all_applications;
ALTER TABLE notifications ADD COLUMN events TEXT NOT NULL DEFAULT '[]';

-- enable_by_default only attached notifications to applications created afterwards, so the
-- explicit associations stay authoritative unless the notification already covers every application.
UPDATE notifications SET all_applications = 0
WHERE all_applications = 1 AND EXISTS (
    SELECT 1 FROM applications a
    WHERE NOT EXISTS (
        SELECT 1 FROM application_notifications an
        WHERE an.application_id = a.id AND an.notification_id = notifications.id
    )
);

DELETE FROM application_notifications
WHERE notification_id IN (SELECT id FROM notifications WHERE all_applications = 1);

-- Existing notifications keep receiving every event that was sent before events became selectable.
UPDATE notifications SET events = '["application.deployment.succeeded","application.deployment.failed","application.image_update.succeeded","application.image_update.failed","application.sync.failed"]';
