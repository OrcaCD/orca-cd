# Application Event History Design

## Goal

Add an application-scoped event history that explains when OrcaCD evaluated or changed an application, what triggered the operation, who or what initiated it, and how it ended.

The history must include manual deployments, initial deployments, commit-driven synchronization, and image pulls. It must preserve meaningful no-change outcomes without filling SQLite with routine polling noise.

## User Experience

The application detail sidebar gains a `History` entry in the existing management group, directly below `Details`. The new page shows a newest-first table with these columns:

- `Timestamp`: when the operation started.
- `Trigger`: manual deployment, initial deployment, commit, or image pull.
- `Triggered by`: the user name for user-driven operations, or the automatic source such as repository polling, repository webhook, GitHub Actions, image polling, image webhook, or application creation.
- `Commit`: short hash and commit message when the operation concerns a commit.
- `Result`: running, successful, failed, or no change.

Result values use status badges. A failed row exposes its error details in an expandable area below the row. The page includes loading, empty, error, and end-of-history states. It initially loads 20 rows and offers `Load more` while additional rows exist.

All user-facing copy is provided in the existing English and German message catalogs.

## Event Semantics

Each logical operation produces at most one history row. The row is created as `running` when OrcaCD starts an asynchronous operation and is updated when its result arrives. Operations that complete in the Hub without contacting an Agent are created directly with their terminal result.

### Types

- `deployment`: an explicit compose deployment, including the deployment performed when an application is created.
- `commit_sync`: evaluation of an application against a repository commit, whether or not it causes a deployment.
- `image_update`: an image pull/update operation.

The frontend maps type and source together to the user-facing trigger label. For example, a `deployment` from `manual` is shown as `Manual`, a `commit_sync` is shown as `Commit`, and an `image_update` is shown as `Image pull`.

### Sources

- `manual`: a user explicitly started a deployment or repository sync.
- `application_created`: application creation started the initial deployment.
- `repository_polling`: scheduled repository polling found a new commit.
- `repository_webhook`: a repository webhook requested synchronization.
- `github_actions`: the GitHub Actions OIDC endpoint requested repository synchronization or image pulling.
- `image_polling`: the Agent's configured image poll found and applied updated images.
- `image_webhook`: an image registry webhook requested an image pull.

### Results

- `running`: dispatched or otherwise still in progress.
- `succeeded`: completed successfully and changed or redeployed the application.
- `failed`: dispatch or processing failed; an error message is recorded when available.
- `no_change`: a new commit was evaluated successfully but did not change the application's compose file.

### Noise Rules

- Scheduled image polling that reports `images_updated = false` does not create an event.
- Scheduled repository polling does not create an event when the resolved commit equals the application's recorded commit.
- A newly observed commit whose compose file is unchanged creates a `no_change` event. This applies to polling and all explicit repository synchronization sources.
- Manual operations, repository webhooks, image webhooks, and GitHub Actions requests are recorded even when repeated or when they produce no change.
- Failed operations are always recorded when OrcaCD can identify the target application.

## Persistence Model

Create an `application_events` table and matching GORM model with:

- `id`: UUIDv7 primary key using the existing model base behavior.
- `application_id`: required foreign key to `applications`, with cascade deletion.
- `request_id`: nullable, unique correlation ID for asynchronous Agent operations.
- `type`: required event type enum.
- `source`: required event source enum.
- `status`: required result enum.
- `actor_user_id`: nullable user ID. The foreign key uses `ON DELETE SET NULL`.
- `actor_name`: nullable snapshot of the initiating user's display name so old history remains understandable after a rename or deletion.
- `commit_hash`: nullable commit hash.
- `commit_message`: nullable commit message.
- `error_message`: nullable failure detail.
- `completed_at`: nullable completion timestamp.
- `created_at` and `updated_at`: existing base timestamps; `created_at` is the operation start time shown in the table.

Add indexes for newest-first application queries and request correlation. Nullable fields are stored as `NULL`, rather than empty strings, so absent metadata is unambiguous.

Creating an event and pruning its application history occur in one database transaction. After insertion, only the newest 1,000 rows by `created_at` and `id` are retained for that application. Event storage is observability: a storage failure is logged but must not prevent or change the underlying deployment, sync, or image operation.

When an application is deleted successfully, its events are removed by the foreign-key cascade. If application deletion fails, its history remains.

## Backend Components and Data Flow

### Event service

A focused application-history package owns event creation, completion, lookup by request ID, pruning, and recovery of interrupted events. Callers provide event metadata rather than writing the model directly.

The creation operation accepts application ID, type, source, optional actor snapshot, and optional commit metadata. For asynchronous operations it also accepts the request ID. Completion updates only a matching running event and publishes an SSE update after persistence succeeds.

### Trigger context

Repository synchronization jobs carry their source and optional actor alongside the repository, application, and commit data. Route handlers derive a manual actor from authenticated JWT claims. Background pollers and external endpoints provide their explicit automatic source.

Deployment and image-pull dispatch generate the request ID before sending the protobuf message. The same ID is persisted with the history row and included in the request. Agent results complete the row using both `request_id` and `application_id`; the existing Agent/application ownership validation remains mandatory for image results.

Periodic image polls originate on the Agent and therefore have no Hub-created running row. When their result arrives, the Hub creates a terminal `image_polling` event only if images were updated or the operation failed. A successful result with no updated images is ignored.

### Commit synchronization

For scheduled repository polling, a job whose resolved commit matches the application's current commit exits before fetching the compose file and creates no event. Other sources continue so explicit requests remain visible.

For a commit that is processed:

1. Create a running `commit_sync` event containing source, actor, hash, and message.
2. Fetch the compose file.
3. If fetching fails, complete the event as failed.
4. If the compose file is unchanged, update the application's commit metadata and complete the event as `no_change`.
5. If it changed, persist the compose data and dispatch a deployment using the same event/request correlation.
6. Complete the event from the Agent's deployment result.

Manual deployments and initial deployments create a running `deployment` event immediately before dispatch. A dispatch failure, including an offline Agent, completes that event as failed.

### Restart recovery

During Hub startup, any event still marked `running` is completed as `failed` with a stable message explaining that the Hub restarted before the operation result was received. This mirrors the existing recovery of applications stuck in a syncing state and prevents permanently running history rows.

## HTTP API and Live Updates

Add this protected endpoint:

`GET /api/v1/applications/:id/events?limit=20&offset=0`

Behavior:

- Verify that the application exists; return `404` when it does not.
- Accept a positive `limit` up to 100 and a non-negative `offset`; return `400` for invalid values.
- Default `limit` to 20 and `offset` to 0.
- Query only the requested application's events, ordered by `created_at DESC, id DESC`.
- Fetch `limit + 1` rows and return `{ "items": [...], "hasMore": boolean }`.
- Return `500` with the project's generic internal error response on database failures.

The response exposes display-ready actor snapshots and event metadata but not the internal request ID. Timestamps use the same API formatting convention as existing routes.

Event creation and terminal updates publish to `/api/v1/applications/:id/events`. The existing frontend SSE prefix matching revalidates all loaded pages for that application's history.

## Frontend Structure

Add a typed application-event API module or extend the focused application API module with the event enums, response type, and paginated response type. The route uses `useSWRInfinite`, following the admin audit-log pattern, with application-specific keys and a page size of 20.

History-specific table columns and table composition live outside `frontend/src/components/ui`; existing shadcn table, badge, button, and collapsible primitives are consumed without modifying generated UI components. The route is file-based under the application detail route tree and is linked from the existing sidebar.

SSE revalidation must preserve the current number of loaded pages. A newly running event appears at the top, and its terminal result replaces that same row after the Agent responds.

## Error Handling and Security

- History reads use the existing authenticated API group.
- Agent results never update an event solely by application ID. A matching request ID and application ID are required, and image results retain the Agent/application ownership check.
- Unknown, duplicate, late, or mismatched results do not mutate another event. They are logged and otherwise ignored.
- Error details are treated as operational data and rendered as text, never as HTML.
- History persistence failures do not block the primary operation.
- Database errors are logged without exposing internals in HTTP responses.

## Verification Strategy

Backend work follows red-green-refactor and includes tests for:

- event creation, terminal completion, and actor/commit metadata;
- request correlation, duplicate completion, and mismatched application IDs;
- successful and failed deployments;
- successful, failed, and no-change commit synchronization;
- repository polling with an unchanged commit producing no event;
- periodic image polling with no image update producing no event;
- updated and failed periodic image polling producing terminal events;
- webhook and GitHub Actions source attribution;
- pruning that retains exactly the newest 1,000 events for one application without affecting another;
- startup recovery of running events;
- cascade deletion with the application;
- API ordering, pagination, application scoping, validation, `404`, and database errors.

The frontend currently has no automated test runner. It is verified with the repository's TypeScript typecheck, lint, formatting check, translation check, and production build. Backend verification runs focused package tests with race detection, then the complete backend test and lint commands.

## Out of Scope

- Searching or filtering history.
- Configurable retention limits.
- Exporting history.
- Recording routine status reports or health checks.
- Recording successful image polls that did not update images.
- Migrating existing audit-log entries into application history.
