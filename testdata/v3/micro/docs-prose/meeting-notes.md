# Meeting Notes — 2024 Q1 Planning

## January 15: Kickoff

**Attendees:** Alex, Jamie, Sam, Taylor

### Decisions
- Migrating from REST to gRPC for internal services by Q2
- SQLite for embedded use cases (edge devices, CLI tools)
- Keeping PostgreSQL for the main web application

### Action Items
- [ ] Alex: Prototype gRPC service with health checks
- [ ] Jamie: Benchmark SQLite WAL mode under concurrent reads
- [ ] Sam: Document the migration plan for REST → gRPC
- [x] Taylor: Set up CI pipeline for new Go services

### Notes
The team discussed whether to use gRPC-Web for browser clients or maintain
a REST gateway. Consensus was to use Connect (buf.build/connect) which
provides both gRPC and REST from a single proto definition.

SQLite's WAL mode was identified as critical for our use case: we need
concurrent reads during indexing operations, and the journal_mode=WAL
pragma allows this without blocking writers.

---

## February 5: Architecture Review

**Attendees:** Alex, Jamie, Sam

### Topic: Connection Pooling Strategy

After benchmarking, we found that `pool_maxsize=10` with `pool_block=False`
gives the best throughput under normal load. Under burst conditions, blocking
mode (`pool_block=True`) prevents connection storms but adds p99 latency.

**Decision:** Use non-blocking pool for internal services (they can handle
connection errors gracefully) and blocking pool for user-facing API (better
to be slow than to error).

### Topic: Error Handling Patterns

Agreed on these patterns:
1. Use sentinel errors (`ErrNotFound`, `ErrAlreadyExists`) for expected conditions
2. Wrap errors with `fmt.Errorf("%w", err)` to preserve the chain
3. Log at the boundary (HTTP handler, gRPC interceptor), not in library code
4. Return gRPC status codes from all RPC handlers, never bare Go errors

### Topic: Retry Logic

Exponential backoff with jitter for external calls. No retries for internal
gRPC calls (let the caller decide). Circuit breaker pattern deferred to Q2.

---

## March 12: Sprint Retrospective

### What Went Well
- SQLite WAL mode benchmarks exceeded expectations (10x concurrent read throughput)
- gRPC migration is ahead of schedule
- Tree-sitter integration for code analysis is working for Python and Go

### What Needs Improvement
- Test coverage for error paths is insufficient
- Docker image size is 800 MB — target is under 500 MB
- Need better documentation for the configuration system

### Action Items
- [ ] Reduce Docker image: switch from golang base to multi-stage with debian-slim
- [ ] Add error path tests for connection pool exhaustion
- [ ] Write architecture.md with configuration reference
