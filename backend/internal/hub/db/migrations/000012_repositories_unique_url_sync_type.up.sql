CREATE UNIQUE INDEX IF NOT EXISTS idx_repositories_url_sync_type ON repositories (url, sync_type);
