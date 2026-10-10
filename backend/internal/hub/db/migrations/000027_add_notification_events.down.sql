INSERT OR IGNORE INTO application_notifications (application_id, notification_id)
SELECT a.id, n.id FROM applications a CROSS JOIN notifications n WHERE n.all_applications = 1;

ALTER TABLE notifications DROP COLUMN events;
ALTER TABLE notifications RENAME COLUMN all_applications TO enable_by_default;
