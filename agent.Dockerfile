FROM golang:1.26-alpine AS builder

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown

RUN apk add --no-cache gcc musl-dev

WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ .

RUN CGO_ENABLED=1 go build \
    -ldflags "-s -w \
      -X github.com/OrcaCD/orca-cd/internal/version.Version=${VERSION} \
      -X github.com/OrcaCD/orca-cd/internal/version.Commit=${COMMIT} \
      -X github.com/OrcaCD/orca-cd/internal/version.BuildDate=${BUILD_DATE}" \
    -o /bin/agent ./cmd/agent

FROM alpine:3.23

COPY --from=builder /bin/agent /usr/local/bin/agent

ENTRYPOINT ["agent"]
