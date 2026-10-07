# Agents.md

This file provides guidance to AI Agents when working with code in this repository.

## What is OrcaCD

OrcaCD is a **GitOps for Docker** platform. It consists of two Go binaries:

- **Hub** — the control plane: REST API (Gin), SQLite persistence, serves the React SPA in production
- **Agent** — a lightweight service that connects to the Hub and executes deployments

The goal of OrcaCD is to be a simple, self-hosted GitOps solution for Docker Compose users. It is **not** trying to replace Kubernetes-based tools like ArgoCD or Flux. The focus should be on a great developer experience, simple installation and usage, but with a secure and robust architecture.

## Prerequisites

Backend: Go 1.26+, golangci-lint v2.11+, `just`, `buf`, `protoc-gen-go`, `gotestsum`.
Frontend: Node.js 24.x or 25.x, pnpm 11+.

The Justfile loads `backend/.env` (copy from `backend/.env.dev.example`).

## Commands

All backend commands are run from `backend/`. All frontend commands from `frontend/`.

### Backend

```sh
just build          # proto + build hub and agent binaries → bin/
just build-hub      # build hub only
just build-agent    # build agent only
just run            # run hub and agent in parallel
just run-hub        # run hub (pass a subcommand: just run-hub help)
just run-agent      # run agent
just lint           # golangci-lint + go mod verify + buf lint
just fmt            # golangci-lint fmt + buf format
just test           # all tests with race detection (gotestsum, -failfast)
just test-coverage  # tests + HTML coverage report
just proto          # regenerate internal/proto/messages.pb.go from messages.proto
```

Tests require `CGO_ENABLED=1` (SQLite, race detector). Running a single package or test:

```sh
CGO_ENABLED=1 go test -v -race ./internal/hub/crypto/...
CGO_ENABLED=1 go test -v -race -run TestName ./internal/hub/routes/
```

### Frontend

```sh
pnpm i --frozen-lockfile        # install dependencies
node --run dev                  # Vite dev server on port 3000
node --run build                # production bundle
node --run typecheck            # TypeScript type check
node --run lint                 # oxlint (type-aware, warnings are errors)
node --run format               # oxfmt
node --run format:check         # oxfmt check only
node --run translations:check   # verify all locales have the same message keys
```

### E2E tests (Playwright, from `e2e/`)

```sh
pnpm i --frozen-lockfile && pnpm run install:browsers
pnpm test       # builds and starts the stack via docker-compose.e2e.yml on :8090
pnpm test:ui    # interactive mode
```

### Local dev (Docker)

```sh
docker compose -f docker-compose.dev.yml up --build
```

Vite proxies `/api` → Hub at `localhost:8080` during development (see `frontend/vite.config.ts`).

## Architecture

```
Frontend (React SPA)
    │  HTTP /api/v1/*  +  SSE for live updates
    ▼
Hub (Go, port 8080)  ◄── WebSocket (encrypted, protobuf) ──►  Agent (Go, on each Docker host)
```

### Hub (`backend/internal/hub/`)

- `server.go` / `handlers.go` — Config, Gin setup, middleware wiring; `RegisterRoutes` defines every `/api/v1` route (public, rate-limited webhooks, protected)
- `routes/` — HTTP handlers
- `models/` + `db/` — GORM models; SQLite with numbered golang-migrate SQL files in `db/migrations/`; also backup/export/restore
- `repositories/`, `applications/` — Git repo sync, polling, image-pull webhooks, deploy queue
- `deployer/` — turns application state into deploy commands sent to agents
- `websocket/` — Hub-side agent connections, worker pool, deploy/delete/status/image-poll messages
- `sse/` + `applicationevents/` — broker pushing events to the frontend
- `auth/`, `oidc/`, `middleware/` — JWT cookies, OIDC login, CSRF/origin/security headers
- `crypto/` — AEGIS-256 encryption for sensitive DB fields
- `notifications/` — outbound notifications

### Agent (`backend/internal/agent/`)

- `agent.go`, `websocket.go` — config and connection lifecycle to the Hub
- `docker/` — Compose deploys, image polling, health watching, mount/policy checks

### Shared (`backend/internal/shared/`, `backend/internal/proto/`)

- `proto/messages.proto` — all Hub↔Agent WebSocket message types (regenerate with `just proto`)
- `wscrypto/` — hybrid ML-KEM-768 + X25519 handshake that derives the WebSocket session key
- `agenttoken/` — agent auth tokens

### Frontend (`frontend/src/`)

- `routes/` — TanStack Router file-based routes; authenticated pages under `_authenticated/`. `routeTree.gen.ts` is generated.
- `lib/` — API clients per domain (`api.ts`, `applications.ts`, …), SSE client, auth
- i18n uses Paraglide: messages in `frontend/messages/{en,de}.json`, compiled into `src/lib/paraglide` (generated). Add new keys to every locale.

**Version info** is injected at build time via `ldflags` into `internal/version/version.go`.

**Production build**: `hub.Dockerfile` is a multi-stage build (Node → Go) that embeds the compiled frontend into the Go binary. `agent.Dockerfile` builds the agent.

## Key technology choices

| Layer             | Choice                                             |
| ----------------- | -------------------------------------------------- |
| Backend framework | Gin + gorilla/websocket + protobuf (buf)           |
| ORM / DB          | GORM + SQLite + golang-migrate                     |
| Encryption        | AEGIS-256 via go-libaegis                          |
| CLI               | Cobra                                              |
| Logging           | Zerolog                                            |
| Frontend router   | TanStack Router (file-based, `src/routes/`)        |
| Forms             | TanStack Form + Zod validation                     |
| UI components     | shadcn/ui (base-nova) + Base UI + Tailwind CSS 4   |
| i18n              | Paraglide (inlang)                                 |
| Linter/formatter  | oxlint + oxfmt (frontend), golangci-lint (backend) |

## Development guidelines

- Use modern language features and best practices for Go and React.
- Write clean, maintainable code with proper error handling and logging.
- Follow the existing code style and conventions.
- Write tests for new features and bug fixes, aiming for good coverage.
- Do not add useless comments, but do add comments to explain complex logic or decisions.
- The repo uses squash merges, so commit history should be clean and focused on the feature/bug being implemented.
- Do not try modify files in frontend/src/components/ui
- Always try to use the Gorm Generics API for database operations
- Schema changes need a new numbered up/down migration pair in `backend/internal/hub/db/migrations/`.
