# Build stage
FROM golang:1.25-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Set working directory
WORKDIR /build

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build arguments for version info
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

# Build the binary
# CGO_ENABLED=0 for pure Go build (modernc.org/sqlite is pure Go)
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildDate=${BUILD_DATE}" \
    -o pganalyzer \
    ./cmd/pganalyzer

# Runtime stage
FROM alpine:3.19

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user for security
RUN addgroup -g 1000 -S pganalyzer && \
    adduser -u 1000 -S pganalyzer -G pganalyzer

# Set working directory
WORKDIR /app

# Create data directory for SQLite database
RUN mkdir -p /app/data && chown -R pganalyzer:pganalyzer /app/data

# Copy binary from builder
COPY --from=builder /build/pganalyzer /app/pganalyzer

# Copy templates (embedded in binary, but keep for reference/override)
# Templates are embedded via embed.FS, so this is optional
# COPY --from=builder /build/internal/web/templates /app/templates
# COPY --from=builder /build/internal/web/static /app/static

# Copy example config
COPY --from=builder /build/configs/config.yaml /app/configs/config.yaml

# Set ownership
RUN chown -R pganalyzer:pganalyzer /app

# Switch to non-root user
USER pganalyzer

# Expose HTTP port
EXPOSE 8080

# Volume for persistent data (SQLite database)
VOLUME ["/app/data"]

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Set environment variables
ENV PGANALYZER_CONFIG=/app/configs/config.yaml

# Run the application
ENTRYPOINT ["/app/pganalyzer"]
