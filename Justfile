import 'backend/Justfile'

set dotenv-path := 'backend/.env'
set working-directory := 'backend'

# Run the frontend dev server once the hub is reachable
[unix]
[working-directory('../frontend')]
run-frontend-after-hub:
    @host="${HOST:-127.0.0.1}"; case "$host" in 0.0.0.0|::|localhost) host=127.0.0.1 ;; esac; until curl -sf "http://$host:${PORT:-8080}/api/v1/health" >/dev/null; do sleep 1; done
    pnpm dev

# Run the frontend dev server once the hub is reachable
[windows]
[working-directory('../frontend')]
run-frontend-after-hub:
    @$hubHost = if ($env:HOST -and $env:HOST -notin '0.0.0.0', '::', 'localhost') { $env:HOST } else { '127.0.0.1' }; $port = if ($env:PORT) { $env:PORT } else { '8080' }; while ($true) { try { Invoke-WebRequest -UseBasicParsing -TimeoutSec 2 "http://${hubHost}:$port/api/v1/health" | Out-Null; break } catch { Start-Sleep -Seconds 1 } }
    pnpm dev

# Run hub and frontend in parallel
[parallel]
dev: run-hub run-frontend-after-hub

# Run hub, agent and frontend in parallel
[parallel]
dev-all: run-hub run-agent run-frontend-after-hub
