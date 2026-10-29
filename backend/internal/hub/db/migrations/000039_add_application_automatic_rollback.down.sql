DROP INDEX IF EXISTS idx_application_deployments_verifying;
ALTER TABLE application_deployments DROP COLUMN verify_until;
ALTER TABLE application_deployments DROP COLUMN previous_deployment_id;

ALTER TABLE applications DROP COLUMN rollback_pause_auto_sync;
ALTER TABLE applications DROP COLUMN rollback_health_timeout_seconds;
ALTER TABLE applications DROP COLUMN rollback_on_unhealthy;
ALTER TABLE applications DROP COLUMN rollback_on_deploy_failure;
ALTER TABLE applications DROP COLUMN rollback_enabled;
