-- Without their refresh token, the grants would end within the hour anyway.
DELETE FROM api_tokens WHERE oauth_client_id IS NOT NULL;

ALTER TABLE api_tokens DROP COLUMN refreshed_at;
ALTER TABLE api_tokens DROP COLUMN refresh_expires_at;
ALTER TABLE api_tokens DROP COLUMN used_refresh_token_hashes;
ALTER TABLE api_tokens DROP COLUMN refresh_token_hash;
ALTER TABLE api_tokens DROP COLUMN oauth_client_id;
