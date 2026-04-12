# syntax=docker/dockerfile:1.6

# FolderMCP v3 Docker image — linux/amd64 glibc
#
# Multi-stage build:
#   Stage 1 (build): Go 1.25 + cgo + libsqlite3-dev → static-ish binary
#   Stage 2 (runtime): debian:bookworm-slim + libsqlite3 + poppler-utils
#
# Usage:
#   docker buildx build --platform linux/amd64 -t foldermcp:v3-m1 -f deploy/v3/Dockerfile .
#   docker run --rm -v /path/to/workspace:/workspace:ro -v /path/to/store:/store foldermcp:v3-m1
#
# The image runs `foldermcp all /workspace` by default, storing the index at /store.
# Override CMD to run individual subcommands:
#   docker run --rm foldermcp:v3-m1 index /workspace
#   docker run --rm foldermcp:v3-m1 serve

# ─── Build stage ──────────────────────────────────────────
# golang:1.25-bookworm matches CI (go-version: '1.25.x').
# Fallback: golang:1.25 or golang:1.25.0-bookworm if the tag moves.
FROM golang:1.25-bookworm AS build

WORKDIR /src

# Install cgo dependencies for mattn/go-sqlite3, tree-sitter, and ONNX Runtime
RUN apt-get update && apt-get install -y --no-install-recommends \
        libsqlite3-dev \
        pkg-config \
        ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Cache dependency downloads
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build with cgo + sqlite_fts5 tags
COPY . .
RUN CGO_ENABLED=1 go build \
        -tags "cgo sqlite_fts5" \
        -ldflags='-s -w' \
        -o /out/foldermcp \
        ./cmd/foldermcp

# ─── Runtime stage ────────────────────────────────────────
FROM debian:bookworm-slim AS runtime

RUN apt-get update && apt-get install -y --no-install-recommends \
        libsqlite3-0 \
        poppler-utils \
        ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/foldermcp /usr/local/bin/foldermcp

# Workspace (read-only mount point for user data)
VOLUME /workspace

# Store (index database, persistent across container restarts)
VOLUME /store

ENV FOLDERMCP_STORE=/store

# Default: run the all-in-one mode (index + serve over Unix socket)
ENTRYPOINT ["/usr/local/bin/foldermcp"]
CMD ["all-v3", "/workspace"]

# Health check: verify the binary responds to --help
HEALTHCHECK --interval=30s --timeout=5s --start-period=60s --retries=3 \
    CMD ["/usr/local/bin/foldermcp", "--help"]

# Metadata
LABEL org.opencontainers.image.title="foldermcp" \
      org.opencontainers.image.description="Local-first semantic index for NAS storage, queryable over MCP" \
      org.opencontainers.image.source="https://github.com/gtm-k/foldermcp" \
      org.opencontainers.image.licenses="Apache-2.0"
