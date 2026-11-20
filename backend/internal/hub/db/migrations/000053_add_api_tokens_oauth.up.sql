-- API tokens an MCP client got through OAuth. The token row is the grant: its
-- access token is replaced on every refresh, so token_hash and expires_at
-- belong to the current access token, while refresh_expires_at ends the grant.
-- The refresh token starts with the id of its row, which is how it is found.
ALTER TABLE api_tokens ADD COLUMN oauth_client_id TEXT;
ALTER TABLE api_tokens ADD COLUMN refresh_token_hash TEXT;
-- The start of the hashes of the refresh tokens the grant had, to tell a copied
-- one when it comes back.
ALTER TABLE api_tokens ADD COLUMN used_refresh_token_hashes TEXT NOT NULL DEFAULT '[]';
ALTER TABLE api_tokens ADD COLUMN refresh_expires_at DATETIME;
ALTER TABLE api_tokens ADD COLUMN refreshed_at DATETIME;
