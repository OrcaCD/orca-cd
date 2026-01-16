.PHONY: all proto build build-hub build-agent docker docker-hub docker-agent clean test

VERSION ?= dev
BUILD_TIME ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

LDFLAGS := -X 'github.com/OrcaCD/orca-cd/internal/config.Version=$(VERSION)' \
           -X 'github.com/OrcaCD/orca-cd/internal/config.BuildTime=$(BUILD_TIME)' \
           -X 'github.com/OrcaCD/orca-cd/internal/config.GitCommit=$(GIT_COMMIT)'

all: proto build

# Generate protobuf code
proto:
	protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		api/proto/hub.proto

# Build all binaries
build: build-hub build-agent

build-hub:
	CGO_ENABLED=1 go build -ldflags="$(LDFLAGS)" -o bin/hub ./cmd/hub

build-agent:
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o bin/agent ./cmd/agent

# Build Docker images
docker: docker-hub docker-agent

docker-hub:
	docker build -f Dockerfile.hub \
		--build-arg VERSION=$(VERSION) \
		--build-arg BUILD_TIME=$(BUILD_TIME) \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		-t orcacd/hub:$(VERSION) .

docker-agent:
	docker build -f Dockerfile.agent \
		--build-arg VERSION=$(VERSION) \
		--build-arg BUILD_TIME=$(BUILD_TIME) \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		-t orcacd/agent:$(VERSION) .

# Run tests
test:
	go test -v ./...

# Clean build artifacts
clean:
	rm -rf bin/
	go clean

# Install protoc plugins (run once)
install-proto-tools:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Development: run hub locally
run-hub:
	go run ./cmd/hub -p 8080 -g 9090

# Development: run agent locally
run-agent:
	go run ./cmd/agent --hub localhost:9090

# Docker compose commands
up:
	docker compose up -d

down:
	docker compose down

logs:
	docker compose logs -f
