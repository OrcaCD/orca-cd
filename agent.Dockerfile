FROM bufbuild/buf:1.68 AS buf

FROM golang:1.26-alpine AS builder

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown

RUN apk add --no-cache gcc musl-dev

WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ .

RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11

COPY --from=buf /usr/local/bin/buf /usr/local/bin/buf
RUN buf generate

RUN CGO_ENABLED=1 go build \
    -ldflags "-s -w \
    -X github.com/OrcaCD/orca-cd/internal/version.Version=${VERSION} \
    -X github.com/OrcaCD/orca-cd/internal/version.Commit=${COMMIT} \
    -X github.com/OrcaCD/orca-cd/internal/version.BuildDate=${BUILD_DATE}" \
    -o /bin/agent ./cmd/agent

FROM alpine:3.23

WORKDIR /app

COPY --from=builder /bin/agent /app/agent

HEALTHCHECK --interval=60s --timeout=5s --start-period=15s --retries=1 \
    CMD ["/app/agent", "healthcheck"]

ENTRYPOINT ["/app/agent"]
