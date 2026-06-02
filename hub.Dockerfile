FROM ghcr.io/pnpm/pnpm:11.5.1 AS install-deps

WORKDIR /app/frontend
COPY frontend/package.json ./
COPY frontend/pnpm-lock.yaml ./
COPY frontend/pnpm-workspace.yaml ./
RUN --mount=type=cache,id=pnpm,target=/pnpm/store \
    pnpm i --frozen-lockfile --store-dir /pnpm/store


FROM node:26-trixie-slim AS frontend-builder

WORKDIR /app/frontend

COPY --from=install-deps /app/frontend/node_modules ./node_modules
COPY frontend/ ./
RUN node --run build

FROM bufbuild/buf:1.70 AS buf

FROM golang:1.26.3-trixie AS builder

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown

WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download && \
    go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11

COPY --from=buf /usr/local/bin/buf /usr/local/bin/buf
COPY backend/ .
RUN buf generate

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 go build \
    -ldflags "-s -w \
    -X github.com/OrcaCD/orca-cd/internal/version.Version=${VERSION} \
    -X github.com/OrcaCD/orca-cd/internal/version.Commit=${COMMIT} \
    -X github.com/OrcaCD/orca-cd/internal/version.BuildDate=${BUILD_DATE}" \
    -o /bin/hub ./cmd/hub

FROM gcr.io/distroless/base-nossl-debian13:nonroot

WORKDIR /app

COPY --from=builder --chown=nonroot:nonroot /bin/hub /app/hub
COPY --from=frontend-builder --chown=nonroot:nonroot /app/frontend/dist ./frontend/dist

EXPOSE 8080

HEALTHCHECK --interval=90s --timeout=5s --start-period=10s --retries=1 CMD [ "/app/hub", "healthcheck" ]

ENTRYPOINT ["/app/hub"]
