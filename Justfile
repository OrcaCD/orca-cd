import 'backend/Justfile'

set dotenv-path := 'backend/.env'
set working-directory := 'backend'

# Run the frontend dev server once the hub is reachable
[unix]
[working-directory('../frontend')]
run-frontend-after-hub:
    @until curl -sf "http://localhost:${PORT:-8080}/api/v1/health" >/dev/null; do sleep 1; done
    pnpm dev

# Run the frontend dev server once the hub is reachable
[windows]
[working-directory('../frontend')]
run-frontend-after-hub:
    @$port = if ($env:PORT) { $env:PORT } else { '8080' }; while ($true) { try { Invoke-WebRequest -UseBasicParsing -TimeoutSec 2 "http://localhost:$port/api/v1/health" | Out-Null; break } catch { Start-Sleep -Seconds 1 } }
    pnpm dev

# Run hub and frontend in parallel
[parallel]
dev: run-hub run-frontend-after-hub

# Run hub, agent and frontend in parallel
[parallel]
dev-all: run-hub run-agent run-frontend-after-hub
