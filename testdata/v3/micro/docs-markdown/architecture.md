# Architecture Overview

## System Design

This document describes the high-level architecture of the data processing
platform. The system follows a layered architecture with clear separation
between data ingestion, processing, storage, and serving.

## Components

### Ingestion Layer

The ingestion layer accepts data from multiple sources:
- REST API endpoints for synchronous writes
- Message queue consumers for asynchronous batch processing
- File watchers for local filesystem changes

Each source is normalized through an **adapter interface** before entering
the processing pipeline. See `data_pipeline.py` for the transformation
framework.

### Processing Pipeline

Data flows through a composable pipeline:

1. **Validation** — Schema validation with configurable strictness
2. **Normalization** — Field standardization (case, encoding, format)
3. **Enrichment** — Cross-reference with external sources
4. **Deduplication** — Content-hash based dedup with configurable window

### Storage Layer

Processed records are stored in SQLite with WAL mode for concurrent reads:

```sql
CREATE TABLE records (
    id TEXT PRIMARY KEY,
    content_hash TEXT NOT NULL,
    data JSONB NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_records_hash ON records(content_hash);
```

### Serving Layer

The gRPC serving layer exposes:
- `SearchRecords` — Full-text search with relevance ranking
- `GetRecord` — Single record retrieval by ID
- `ListRecords` — Paginated listing with filters

## Configuration

Configuration follows a hierarchy: defaults → config file → environment variables.
See `config.go` for the implementation. Environment variables override file-based
config using the pattern `MYAPP_SECTION_KEY`.

## Deployment

The system runs as a single binary with subcommands:
- `serve` — Start the gRPC server
- `index` — Run the processing pipeline
- `all` — Combined mode (index + serve)

Docker deployment uses a multi-stage build with Debian Bookworm for glibc
compatibility.
