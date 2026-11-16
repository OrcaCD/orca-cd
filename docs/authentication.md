# Authentication

Local accounts sign in with passkeys. OpenID Connect (OIDC) remains available for SSO, including password or additional authentication policies enforced by your identity provider.

## Deployment requirements

Set `APP_URL` to the exact origin used in your browser, including a nonstandard port. Passkeys require HTTPS and a domain name. HTTP is supported for local development on `localhost`. IP addresses are not supported as relying party IDs. Reverse proxies must preserve access to the configured browser origin. `DISABLE_LOCAL_AUTH=true` disables local passkey sign-in and enrollment; OIDC continues to work.

The domain is part of each passkey. Moving the Hub to a different domain requires enrollment of new passkeys. Keep `APP_SECRET` and the database together in backups: stored credentials are encrypted with the application secret. Use the Hub's `key rotate` command when rotating that secret.

## Initial setup

Open the setup link printed by the Hub, enter the administrator's name and email, and create a passkey using a device, security key, or password manager. The administrator account is created only after the passkey is verified. Register a second passkey in **Settings → Security** as a backup.

## Upgrade from passwords

The migration preserves users, roles and OIDC identities, marks existing password accounts as local accounts, and removes password hashes. Native password sign-in and password changes are removed. Existing sessions remain valid; local users with an active session can add passkeys in **Settings → Security** before signing out. Users without an active session need an enrollment code from an administrator or the recovery command below. OIDC sign-in continues to work for linked accounts.

Back up the database and `APP_SECRET` before upgrading. Downgrading cannot restore deleted password hashes; use the previous version's password reset procedure after restoring a compatible backup.

## Add users and recover access

Administrators create local users from **Admin → Users**. Copy the returned enrollment code and deliver it privately to the intended user. They select **Set up a passkey** on the sign-in page, enter the code and create their passkey. Codes expire after 24 hours and are usable once. Retrying a canceled passkey prompt does not consume a code; successful enrollment does.

To recover an existing local account, an administrator selects **Reset passkeys** when editing that user. This immediately revokes all of that user's passkeys and sessions, invalidates any previous enrollment code, and returns a replacement code. SSO-only users must recover access through their identity provider.

If no administrator can sign in, run this command against the Hub's existing data directory and environment:

```sh
hub reset-passkeys --email admin@example.com
# Or select the account by ID:
hub reset-passkeys --id <user-id>
```

For Docker Compose installations, run the command in the Hub container (replace `hub` with your service name):

```sh
docker compose exec hub /app/hub reset-passkeys --email admin@example.com
```

The command prints the single-use enrollment code. It replaces `reset-password` and revokes that account's credentials and sessions. Treat the code as a temporary account secret. Recovery is unavailable in demo mode.

## Passkey management

Local users can name, add and delete their own passkeys in **Settings → Security**. At least one passkey must remain; use administrator recovery if all authenticators are lost. Up to 20 passkeys are supported per account. Each passkey sign-in and enrollment requires device verification, such as a biometric or authenticator PIN. Authentication challenges expire after five minutes and are single-use. Restarting the Hub cancels pending prompts, so simply retry the operation.

Demo mode offers a public **Explore demo** button and blocks changes. It does not create passkeys or accept passwords.
