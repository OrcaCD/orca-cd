FROM --platform=$BUILDPLATFORM ghcr.io/pnpm/pnpm:11.10.0 AS install-deps

WORKDIR /app/frontend
COPY frontend/package.json ./
COPY frontend/pnpm-lock.yaml ./
COPY frontend/pnpm-workspace.yaml ./
RUN --mount=type=cache,id=pnpm,target=/pnpm/store \
    pnpm i --frozen-lockfile --store-dir /pnpm/store


FROM --platform=$BUILDPLATFORM node:26-trixie-slim AS frontend-builder

WORKDIR /app/frontend

COPY --from=install-deps /app/frontend/node_modules ./node_modules
COPY frontend/ ./
RUN node --run build

FROM --platform=$BUILDPLATFORM golang:1.26.5-trixie AS builder

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown
ARG TARGETARCH
ARG BUILDARCH

WORKDIR /src

# Cross-compiling cgo needs a gcc/libc toolchain for the target arch when it
# differs from the build host, so the whole toolchain runs natively instead
# of under QEMU emulation.
RUN if [ "$TARGETARCH" != "$BUILDARCH" ]; then \
        apt-get update && \
        apt-get install -y --no-install-recommends "crossbuild-essential-$TARGETARCH" && \
        rm -rf /var/lib/apt/lists/*; \
    fi

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ .

RUN CGO_ENABLED=1 GOOS=linux GOARCH=$TARGETARCH \
    CC=$( [ "$TARGETARCH" = "arm64" ] && echo aarch64-linux-gnu-gcc || echo gcc ) \
    go build \
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
