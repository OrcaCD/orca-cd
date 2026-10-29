DROP INDEX IF EXISTS idx_application_events_deployment_id;
ALTER TABLE application_events DROP COLUMN deployment_id;
ALTER TABLE applications DROP COLUMN current_deployment_id;
DROP INDEX IF EXISTS idx_application_deployments_app_deployed;
DROP INDEX IF EXISTS idx_application_deployments_request_id;
DROP TABLE IF EXISTS application_deployments;
