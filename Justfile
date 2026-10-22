import 'backend/Justfile'

set dotenv-path := 'backend/.env'
set working-directory := 'backend'

# Run the frontend dev server on port 3000
[working-directory('../frontend')]
run-frontend:
    pnpm dev

# Run hub and frontend in parallel
[parallel]
dev: run-hub run-frontend

# Run hub, agent and frontend in parallel
[parallel]
dev-all: run-hub run-agent run-frontend
