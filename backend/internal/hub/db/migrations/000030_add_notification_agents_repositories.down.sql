DROP INDEX IF EXISTS idx_repository_notifications_notification_id;
DROP INDEX IF EXISTS idx_repository_notifications_repository_id;
DROP TABLE IF EXISTS repository_notifications;
DROP INDEX IF EXISTS idx_agent_notifications_notification_id;
DROP INDEX IF EXISTS idx_agent_notifications_agent_id;
DROP TABLE IF EXISTS agent_notifications;
ALTER TABLE notifications DROP COLUMN all_repositories;
ALTER TABLE notifications DROP COLUMN all_agents;

-- Drop the events introduced alongside this migration; the previous version rejects unknown events on update.
UPDATE notifications SET events = (
    SELECT json_group_array(value) FROM json_each(notifications.events)
    WHERE value NOT IN (
        'application.health.unhealthy', 'application.health.recovered',
        'agent.offline', 'agent.online',
        'repository.sync.failed', 'repository.sync.recovered'
    )
);
