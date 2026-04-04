# Changelog

All notable changes to FolderMCP will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added
- Python introspector with AST parsing and type hint support
- TypeScript/JavaScript introspector with JSDoc support
- OpenAPI spec introspector with automatic risk labeling
- Shell script introspector
- Auto-dependency resolution using uv with curated import-to-package mapping
- Review/approval CLI with batch mode and interactive modes (dev/team/production)
- MCP server with stdio and Streamable HTTP transports
- API key authentication for team mode
- Self-signed TLS certificate generation
- Developer Studio web UI with tool catalog, audit log, and status dashboard
- @foldermcp.tool Python decorator for explicit tool registration
- Subprocess sandbox executor with timeout and output size limits
- Secret redaction in tool output and audit logs
- Claude Desktop auto-configuration via `foldermcp connect`
- Docker and Cloud Run deployment adapters with --dry-run
- A2A agent-card.json export
- `foldermcp doctor` diagnostics with `--fix` auto-remediation
- Structured JSON audit logging
- GitHub Actions CI for Linux, macOS, and Windows
- Cross-component integration test suite (20+ scenarios)
- 3 example repos: Python, OpenAPI, Shell scripts

### Fixed
- Test command now enforces deny-by-default (rejects pending/disabled tools)
- Python risk heuristics auto-classify destructive/side-effect functions
- Unified secret redaction across output and audit logs
- Graceful shutdown for serve command (SIGINT/SIGTERM)
- Dockerfile now runs as non-root user with HEALTHCHECK
- Audit log rotation at 10MB
- Live tool state reload during serve (no stale state)
- Proper TOML parser for pyproject.toml
- Async Python function execution via asyncio.run
- OpenAPI content sniffing (skips non-OpenAPI YAML/JSON)
- Duplicate tool names get auto-suffix instead of silent overwrite
- Symlink safety in directory scanner

### Security
- Deny-by-default: no tool is invocable until explicitly approved
- Per-tool risk labeling (read_only, side_effects, destructive, network)
- Output sanitization for AWS keys, GitHub tokens, API keys, private keys
- Subprocess isolation with configurable timeouts and output limits
