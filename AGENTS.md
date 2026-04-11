# Agents.md

This file provides guidance to AI Agents when working with code in this repository.

## What is OrcaCD

OrcaCD is a **GitOps for Docker** platform. It consists of two Go binaries:

- **Hub** — the control plane: REST API (Gin), SQLite persistence, serves the React SPA in production
- **Agent** — a lightweight service that connects to the Hub and executes deployments

The goal of OrcaCD is to be a simple, self-hosted GitOps solution for Docker Compose users. It is **not** trying to replace Kubernetes-based tools like ArgoCD or Flux. The focus should be on a great developer experience, simple installation and usage, but with a secure and robust architecture.

## Prerequisites

Go 1.26+, golangci-lint v2.11+, `just`, Node.js 24.x or 25.x, npm.

## Commands

All backend commands are run from `backend/`. All frontend commands from `frontend/`.

### Backend

```sh
just build          # build both hub and agent binaries → bin/
just build-hub      # build hub only
just build-agent    # build agent only
just run-hub        # run hub
just run-agent      # run agent
just lint           # golangci-lint + go mod verify
just fmt            # format with golangci-lint
just test           # run all tests with race detection
just test-coverage  # tests + HTML coverage report
just proto          # generate Go code from .proto files, used for WebSocket messages
```

Running a single test package:

```sh
go test -v -race ./internal/hub/crypto/...
```

### Frontend

```sh
npm ci                 # install dependencies
node --run dev         # Vite dev server on port 3000
node --run build       # production bundle
node --run typecheck   # TypeScript type check
node --run lint        # oxlint
node --run format      # oxfmt
```

### Local dev (Docker)

```sh
docker compose -f docker-compose.dev.yaml up --build
```

Vite proxies `/api` → Hub at `localhost:8080` during development (see `frontend/vite.config.ts`).

## Architecture

```
Frontend (React SPA)
    │  HTTP /api/*
    ▼
Hub (Go, port 8080)
    ├─ auth/        JWT + password hashing, cookies
    ├─ crypto/      AEGIS-256 encryption for sensitive DB fields
    ├─ db/          SQLite + golang-migrate + GORM
    ├─ models/      GORM models (Agent, …)
    ├─ middleware/  CSRF, security headers, origin validation, authentication
    ├─ routes/      /api/v1/ — health, auth endpoints
    ├─ websocket/   Hub-side WebSocket handler + worker pool
    ├─ server.go    Config, Gin setup, middleware wiring
    ├─ handlers.go  registers all routes
    └─ url.go       APP_URL parsing and validation

Agent (Go)
    ├─ agent.go     Config, connection lifecycle
    ├─ websocket.go WebSocket client connecting to Hub
    └─ url.go       Hub URL parsing and validation
```

**Version info** is injected at build time via `ldflags` into `internal/version/version.go`.

**Production build**: `hub.Dockerfile` is a multi-stage build (Node → Go) that embeds the compiled frontend into the Go binary.

## Key technology choices

| Layer             | Choice                                             |
| ----------------- | -------------------------------------------------- |
| Backend framework | Gin + gorilla/websocket                            |
| ORM / DB          | GORM + SQLite + golang-migrate                     |
| Encryption        | AEGIS-256 via go-libaegis                          |
| CLI               | Cobra                                              |
| Logging           | Zerolog                                            |
| Frontend router   | TanStack Router (file-based, `src/routes/`)        |
| Forms             | TanStack Form + Zod validation                     |
| UI components     | shadcn/ui + Radix UI + Tailwind CSS 4              |
| Linter/formatter  | oxlint + oxfmt (frontend), golangci-lint (backend) |

## Development guidelines

- Use modern language features and best practices for Go and React.
- Write clean, maintainable code with proper error handling and logging.
- Follow the existing code style and conventions.
- Write tests for new features and bug fixes, aiming for good coverage.
- Do not add useless comments, but do add comments to explain complex logic or decisions.
- The repo uses squash merges, so commit history should be clean and focused on the feature/bug being implemented.
- Do not try modify files in frontend/src/components/ui