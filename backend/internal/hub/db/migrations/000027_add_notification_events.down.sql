ALTER TABLE notifications DROP COLUMN events;
ALTER TABLE notifications RENAME COLUMN all_applications TO enable_by_default;
