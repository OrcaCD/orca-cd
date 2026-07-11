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
    -o /bin/agent ./cmd/agent

# Not using nonroot variant as agent has access to the docker socket which is basically root access
# Changing to nonroot user would complicate setup for users with minimal security benefits
FROM gcr.io/distroless/base-nossl-debian13:latest

WORKDIR /app

COPY --from=builder /bin/agent /app/agent

HEALTHCHECK --interval=60s --timeout=5s --start-period=15s --retries=1 \
    CMD ["/app/agent", "healthcheck"]

ENTRYPOINT ["/app/agent"]
