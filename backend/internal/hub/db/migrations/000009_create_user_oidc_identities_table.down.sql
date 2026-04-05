UPDATE users
SET oidc_issuer = (
        SELECT uoi.issuer
        FROM user_oidc_identities uoi
        WHERE uoi.user_id = users.id
        ORDER BY uoi.created_at ASC
        LIMIT 1
    ),
    oidc_subject = (
        SELECT uoi.subject
        FROM user_oidc_identities uoi
        WHERE uoi.user_id = users.id
        ORDER BY uoi.created_at ASC
        LIMIT 1
    )
WHERE EXISTS (
    SELECT 1 FROM user_oidc_identities uoi WHERE uoi.user_id = users.id
);

DROP INDEX IF EXISTS idx_user_oidc_identities_provider_id;
DROP INDEX IF EXISTS idx_user_oidc_identities_user_id;
DROP INDEX IF EXISTS idx_user_oidc_identities_user_provider;
DROP INDEX IF EXISTS idx_user_oidc_identities_issuer_subject;
DROP TABLE IF EXISTS user_oidc_identities;
