# Build stage for frontend
FROM node:24-trixie-slim AS frontend-builder

WORKDIR /app/frontend

# Copy frontend package files
COPY frontend/package*.json ./

# Install dependencies
RUN npm install

# Copy frontend source
COPY frontend/ ./

# Build frontend
RUN npm run build

# Build stage for Go backend
FROM golang:1.25-alpine AS backend-builder

WORKDIR /app

# Install build dependencies for SQLite
RUN apk add --no-cache gcc musl-dev sqlite-dev

# Copy go mod files
COPY go.mod go.sum* ./

# Download dependencies
RUN go mod download

# Copy source code
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY main.go ./

# Define build arguments
ARG VERSION=dev
ARG BUILD_TIME=unknown
ARG GIT_COMMIT=unknown

# Build the Go application with version information
RUN CGO_ENABLED=1 GOOS=linux go build \
    -ldflags="-X 'orca-cd/internal/config.Version=${VERSION}' \
              -X 'orca-cd/internal/config.BuildTime=${BUILD_TIME}' \
              -X 'orca-cd/internal/config.GitCommit=${GIT_COMMIT}'" \
    -o server .

# Final stage
FROM alpine:3.22

RUN apk --no-cache add ca-certificates sqlite-libs

WORKDIR /root/

# Copy the Go binary from backend builder
COPY --from=backend-builder /app/server .

# Copy the built frontend from frontend builder
COPY --from=frontend-builder /app/frontend/dist ./frontend/dist

# Create data directory for SQLite database
RUN mkdir -p ./data

VOLUME ["/data"]

# Expose port 8080
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 CMD ["orca-cd", "healthcheck"]

# Run the server (root command starts the API)
CMD ["./server"]
