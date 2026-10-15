CREATE INDEX IF NOT EXISTS idx_application_events_created
    ON application_events(created_at DESC, id DESC);
