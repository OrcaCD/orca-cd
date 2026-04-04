FROM node:25-trixie-slim AS frontend-builder

WORKDIR /app/frontend

COPY frontend/package*.json ./

RUN npm ci --ignore-scripts

COPY frontend/ ./

RUN npm run build

FROM golang:1.26-alpine AS builder

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown

RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ .

RUN CGO_ENABLED=1 go build \
    -ldflags "-s -w \
      -X github.com/OrcaCD/orca-cd/internal/version.Version=${VERSION} \
      -X github.com/OrcaCD/orca-cd/internal/version.Commit=${COMMIT} \
      -X github.com/OrcaCD/orca-cd/internal/version.BuildDate=${BUILD_DATE}" \
    -o /bin/hub ./cmd/hub

FROM alpine:3.23

WORKDIR /app

RUN apk add --no-cache ca-certificates sqlite-libs

COPY --from=builder /bin/hub /app/hub
COPY --from=frontend-builder /app/frontend/dist ./frontend/dist

EXPOSE 8080

HEALTHCHECK --interval=90s --timeout=5s --start-period=10s --retries=3 CMD [ "/app/hub", "healthcheck" ]

ENTRYPOINT ["/app/hub"]
