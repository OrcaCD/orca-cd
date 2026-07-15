ALTER TABLE repositories ADD COLUMN github_actions_oidc_allow_repo_sync INTEGER NOT NULL DEFAULT 1;
ALTER TABLE repositories ADD COLUMN github_actions_oidc_allow_image_sync INTEGER NOT NULL DEFAULT 1;
ALTER TABLE repositories ADD COLUMN github_actions_oidc_allowed_branches TEXT NOT NULL DEFAULT '[]';
ALTER TABLE repositories ADD COLUMN github_actions_oidc_allowed_workflows TEXT NOT NULL DEFAULT '[]';
