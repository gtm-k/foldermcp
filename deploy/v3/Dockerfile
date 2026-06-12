# syntax=docker/dockerfile:1.6

# FolderMCP v3 Docker image — linux/amd64 glibc
#
# Multi-stage build:
#   Stage 1 (build): Go 1.25 + cgo + libsqlite3-dev → static-ish binary
#                    (also downloads the ONNX Runtime shared library)
#   Stage 2 (runtime): debian:bookworm-slim + libsqlite3 + poppler-utils
#                      + ONNX Runtime 1.24.1 (semantic embeddings)
#
# Usage:
#   docker buildx build --platform linux/amd64 -t foldermcp:v3-m1 -f deploy/v3/Dockerfile .
#   docker run --rm -v /path/to/workspace:/workspace:ro -v /path/to/store:/store \
#       -v /path/to/model-dir:/models:ro foldermcp:v3-m1
#
# The image runs `foldermcp all /workspace` by default, storing the index at /store.
# Override CMD to run individual subcommands:
#   docker run --rm foldermcp:v3-m1 index /workspace
#   docker run --rm foldermcp:v3-m1 serve
#
# ── Embedding model provisioning (REQUIRED for semantic indexing) ──
# The ~90 MB all-MiniLM-L6-v2 model is NOT baked into the image. Mount a
# directory containing `model.onnx` + `tokenizer.json` at /models
# (FOLDERMCP_MODEL_DIR points there). Download once on the host:
#
#   mkdir -p model && \
#   curl -L -o model/model.onnx \
#     https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/onnx/model.onnx && \
#   curl -L -o model/tokenizer.json \
#     https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/tokenizer.json
#   docker run --rm -v "$PWD/model:/models:ro" ... foldermcp:v3-m1
#
# Without the model mount the server still runs, but semantic indexing is
# disabled (`foldermcp all` logs "indexer not started: embedding model file
# missing at /models/model.onnx").

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

# ONNX Runtime shared library for the runtime stage (same 1.24.1 release
# used in WSL/dev). Downloaded here because bookworm-slim has no curl.
ARG ORT_VERSION=1.24.1
RUN curl -fsSL -o /tmp/onnxruntime.tgz \
        "https://github.com/microsoft/onnxruntime/releases/download/v${ORT_VERSION}/onnxruntime-linux-x64-${ORT_VERSION}.tgz" \
    && tar -xzf /tmp/onnxruntime.tgz -C /tmp \
    && mkdir -p /out/lib \
    && cp "/tmp/onnxruntime-linux-x64-${ORT_VERSION}/lib/libonnxruntime.so.${ORT_VERSION}" /out/lib/ \
    && rm -rf /tmp/onnxruntime.tgz "/tmp/onnxruntime-linux-x64-${ORT_VERSION}"

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

# ONNX Runtime 1.24.1 shared library (mirrors the WSL/dev layout:
# /usr/lib/onnxruntime.so → libonnxruntime.so.1.24.1). FOLDERMCP_ORT_LIB
# pins the exact path so dlopen never depends on linker search order.
ARG ORT_VERSION=1.24.1
COPY --from=build /out/lib/libonnxruntime.so.${ORT_VERSION} /usr/local/lib/
RUN ln -s "/usr/local/lib/libonnxruntime.so.${ORT_VERSION}" /usr/local/lib/libonnxruntime.so \
    && ln -s "/usr/local/lib/libonnxruntime.so.${ORT_VERSION}" /usr/lib/onnxruntime.so \
    && ldconfig

# Workspace (read-only mount point for user data)
VOLUME /workspace

# Store (index database, persistent across container restarts)
VOLUME /store

# Embedding model mount point (model.onnx + tokenizer.json; see header)
VOLUME /models

ENV FOLDERMCP_STORE=/store \
    FOLDERMCP_MODEL_DIR=/models \
    FOLDERMCP_ORT_LIB=/usr/lib/onnxruntime.so

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
