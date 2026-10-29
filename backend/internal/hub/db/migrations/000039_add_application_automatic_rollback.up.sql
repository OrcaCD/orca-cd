ALTER TABLE applications ADD COLUMN rollback_enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE applications ADD COLUMN rollback_on_deploy_failure INTEGER NOT NULL DEFAULT 1;
ALTER TABLE applications ADD COLUMN rollback_on_unhealthy INTEGER NOT NULL DEFAULT 1;
ALTER TABLE applications ADD COLUMN rollback_health_timeout_seconds INTEGER NOT NULL DEFAULT 300;
ALTER TABLE applications ADD COLUMN rollback_pause_auto_sync INTEGER NOT NULL DEFAULT 1;

-- No foreign key, see 000037. Deleted deployments clear the reference.
ALTER TABLE application_deployments ADD COLUMN previous_deployment_id TEXT;
ALTER TABLE application_deployments ADD COLUMN verify_until DATETIME;
CREATE INDEX IF NOT EXISTS idx_application_deployments_verifying
    ON application_deployments(verify_until) WHERE status = 'verifying';
