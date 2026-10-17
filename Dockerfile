# syntax=docker/dockerfile:1.7

# --- Builder stage ---
FROM golang:1.22-alpine AS builder
WORKDIR /src

# Install build deps (git for fetching modules)
RUN apk add --no-cache git

# Cache modules
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy the rest of the source
COPY . .

# Build a static binary
RUN --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags "-s -w" -o /out/aos ./cmd/aos
RUN --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags "-s -w" -o /out/aos-mocks ./cmd/mocks


# --- Runtime stage ---
FROM alpine:3.20

# Install minimal runtime dependencies
RUN apk add --no-cache ca-certificates tzdata wget

# Create non-root user and group
RUN addgroup -S app && adduser -S -G app -u 10001 app

WORKDIR /app

# Copy binary and required runtime assets
COPY --from=builder /out/aos /usr/local/bin/aos
COPY --from=builder /out/aos-mocks /usr/local/bin/aos-mocks
COPY definitions ./definitions
COPY templates ./templates

# Ensure files are owned by non-root user
RUN chown -R app:app /app /usr/local/bin/aos /usr/local/bin/aos-mocks

# Expose default port used by the application
EXPOSE 9090

# Healthcheck hitting the live endpoint
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -q -O - http://127.0.0.1:9090/health/live >/dev/null 2>&1 || exit 1

# Run as non-root
USER app

# Default command
CMD ["/usr/local/bin/aos"]
