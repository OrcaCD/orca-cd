import 'backend/Justfile'

set dotenv-path := 'backend/.env'
set working-directory := 'backend'

# Run the frontend dev server once the hub is reachable
[working-directory('../frontend')]
run-frontend-after-hub:
    @until curl -sf "http://localhost:${PORT:-8080}/api/v1/health" >/dev/null; do sleep 1; done
    pnpm dev

# Run hub and frontend in parallel
[parallel]
dev: run-hub run-frontend-after-hub

# Run hub, agent and frontend in parallel
[parallel]
dev-all: run-hub run-agent run-frontend-after-hub
