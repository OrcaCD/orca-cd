-- Whether applications can assign themselves to the notification with
-- x-orcacd in their compose file.
ALTER TABLE notifications ADD COLUMN compose_assignable INTEGER NOT NULL DEFAULT 0;
