# OrcaCD

A GitOps continuous delivery platform with a hub/agent architecture.

## Architecture

OrcaCD uses a hub/agent architecture where:

- **Hub**: Central server that serves the frontend, provides REST API, manages the database, and coordinates agents via gRPC streaming
- **Agent**: Lightweight worker that connects to the hub and executes tasks (deployments, builds, shell commands)

```
┌─────────────────┐         gRPC Streaming         ┌─────────────────┐
│                 │◄──────────────────────────────►│                 │
│      Hub        │                                │     Agent       │
│                 │                                │                 │
│  - REST API     │         gRPC Streaming         │  - Task Exec    │
│  - Frontend     │◄──────────────────────────────►│  - Deploy       │
│  - Database     │                                │  - Build        │
│  - gRPC Server  │         gRPC Streaming         │                 │
│                 │◄──────────────────────────────►│     Agent       │
└─────────────────┘                                └─────────────────┘
```

## Quick Start

### Using Docker Compose

```bash
docker compose up -d
```

This starts:
- Hub on port 8080 (HTTP) and 9090 (gRPC)
- One agent connected to the hub

### Building Locally

```bash
# Install dependencies and generate protobuf code
make proto

# Build both binaries
make build

# Run hub
./bin/hub --port 8080 --grpc-port 9090

# Run agent (in another terminal)
./bin/agent --hub localhost:9090
```

## Docker Images

### Build Images

```bash
# Build both images
make docker

# Or individually
make docker-hub
make docker-agent
```

### Hub Image (Dockerfile.hub)

The hub image includes:
- Go backend binary
- Frontend static files
- SQLite support

```bash
docker build -f Dockerfile.hub -t orcacd/hub:latest .
docker run -p 8080:8080 -p 9090:9090 orcacd/hub:latest
```

### Agent Image (Dockerfile.agent)

The agent image is minimal and includes:
- Go agent binary
- Docker CLI, kubectl, git for task execution

```bash
docker build -f Dockerfile.agent -t orcacd/agent:latest .
docker run orcacd/agent:latest --hub hub:9090
```

## Configuration

### Hub

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `--port, -p` | `PORT` | `8080` | HTTP port for REST API and frontend |
| `--grpc-port, -g` | `GRPC_PORT` | `9090` | gRPC port for agent communication |
| | `DB_DRIVER` | `sqlite` | Database driver (`sqlite` or `postgres`) |
| | `DB_PATH` | `./data/app.db` | SQLite database path |
| | `DATABASE_URL` | | PostgreSQL connection string |

### Agent

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `--hub, -H` | `HUB_ADDR` | `localhost:9090` | Hub gRPC address |
| `--id, -i` | `AGENT_ID` | (auto-generated) | Agent ID |

## API Endpoints

### Health
- `GET /api/health` - Health check

### Messages
- `GET /api/messages` - List messages
- `POST /api/messages` - Create message

### Agents
- `GET /api/agents` - List connected agents
- `GET /api/agents/:id` - Get agent details

## Development

```bash
# Install protobuf tools
make install-proto-tools

# Generate protobuf code
make proto

# Run hub locally
make run-hub

# Run agent locally (in another terminal)
make run-agent

# Run tests
make test
```

## Project Structure

```
├── api/proto/           # gRPC protobuf definitions
├── cmd/
│   ├── hub/            # Hub entrypoint
│   └── agent/          # Agent entrypoint
├── internal/
│   ├── agent/          # Agent implementation
│   │   ├── grpc/       # Agent gRPC client
│   │   └── executor/   # Task executor
│   ├── hub/            # Hub implementation
│   │   └── grpc/       # Hub gRPC server
│   ├── config/         # Configuration
│   ├── controllers/    # HTTP controllers
│   ├── database/       # Database connection
│   ├── middleware/     # HTTP middleware
│   ├── models/         # Data models
│   └── utils/          # Utilities
├── frontend/           # React frontend
├── Dockerfile.hub      # Hub Docker image
├── Dockerfile.agent    # Agent Docker image
└── docker-compose.yml  # Docker Compose config
```
