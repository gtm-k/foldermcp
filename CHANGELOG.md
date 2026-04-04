# Changelog

All notable changes to FolderMCP will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added
- Python introspector with AST parsing and type hint support
- TypeScript/JavaScript introspector with JSDoc and npm dependency inference
- OpenAPI spec introspector with automatic risk labeling
- Shell script introspector
- MCP Resources support for PDFs, images, documents (`.md`, `.txt`), CSV, and data files
- NAS/shared drive core: workspace manager, split storage, shared approvals, content hashing
- Auto-detects network filesystem type (SMB, NFS, Azure Files) at runtime
- `foldermcp diff` command to preview changes before re-scanning
- `foldermcp connect` support for Claude Desktop, Claude Code, Cursor, VS Code, and Windsurf
- `foldermcp init --template` scaffolding for Python, OpenAPI, and Shell example projects
- `foldermcp connect --snippet` for copyable JSON config output
- `foldermcp review --approve-all` for bulk approval in batch mode
- `foldermcp review --confirm` to set tools as requires_confirmation
- `foldermcp test --all` to smoke-test all enabled tools
- `foldermcp test --force` to bypass state check during testing
- `foldermcp catalog --type` filter for tools vs resources
- `foldermcp logs --follow` and `--json` output modes
- `foldermcp serve --watch` with polling-based file watcher (NAS-compatible)
- `foldermcp serve --profile` to serve only tools in a named profile
- Auto-dependency resolution using uv with curated import-to-package mapping
- Review/approval CLI with batch mode and interactive modes (dev/team/production)
- MCP server with stdio and Streamable HTTP transports
- API key authentication for team mode
- Self-signed TLS certificate generation
- Developer Studio web UI with tool catalog, audit log, and status dashboard
- `@foldermcp.tool` Python decorator for explicit tool registration
- Subprocess sandbox executor with timeout and output size limits
- Secret redaction in tool output and audit logs
- Docker and Cloud Run deployment adapters with `--dry-run` preview
- A2A agent-card.json export via `foldermcp export a2a`
- `foldermcp doctor` diagnostics with `--fix` auto-remediation
- Structured JSON audit logging with 10MB rotation
- Caller identity tracking in audit logs
- Lifecycle state machine for tool approval transitions
- Graceful shutdown for serve command (SIGINT/SIGTERM)
- Health (`/healthz`), readiness (`/readyz`), and metrics (`/metrics`) HTTP endpoints
- Live tool state reload during serve (no stale state)
- Shell completion for bash, zsh, fish, and PowerShell
- GitHub Actions CI for Linux, macOS, and Windows
- Cross-component integration test suite (20+ scenarios)
- 3 example repos: Python, OpenAPI, Shell scripts
- Tool routing with profiles and max_tools_per_context configuration
- Profile examples in config template

### Fixed
- Test command enforces deny-by-default (rejects pending/disabled tools)
- Python risk heuristics auto-classify destructive/side-effect functions
- Unified secret redaction across output and audit logs
- Graceful shutdown for serve command (SIGINT/SIGTERM)
- Dockerfile runs as non-root user with HEALTHCHECK
- Audit log rotation at 10MB
- Live tool state reload during serve (no stale state)
- Proper TOML parser for pyproject.toml
- Async Python function execution via asyncio.run
- OpenAPI content sniffing (skips non-OpenAPI YAML/JSON)
- Duplicate tool names get auto-suffix instead of silent overwrite
- Symlink safety in directory scanner
- Path traversal guard hardened with symlink resolution
- API key masking in logs and error output
- Developer Studio binds to localhost only (not 0.0.0.0)
- `.env` files excluded from scanning by default
- Symlink validation before following links
- `logs --json` outputs raw JSON lines instead of formatted text
- UNC path normalization for Windows network drives
- Diff command content detection uses content hashing
- Doctor command references shared approvals correctly
- `__pycache__` directories excluded from scanning
- macOS build fix for Fstypename type
- Resolved all golangci-lint issues (37 errcheck, 4 staticcheck)
- Atomic config write to prevent corruption
- Connect `--mode` flag propagated correctly
- Test exit codes match tool execution result
- Review warnings for invalid tool names
- Init validation for existing workspaces
- Serve uses config values for port and mode
- Empty YAML handling without panic
- Deduplicated findPython across packages
- Sanitized audit parameters (no raw secrets in log entries)
- Validated state update transitions
- Windows venv path resolution
- Unicode-safe output truncation
- Duplicate tool name warnings in scanner

### Security
- Deny-by-default: no tool is invocable until explicitly approved
- Per-tool risk labeling (read_only, side_effects, destructive, network)
- Output sanitization for AWS keys, GitHub tokens, API keys, private keys
- Subprocess isolation with configurable timeouts and output limits
- Path traversal protection with symlink resolution
- API key masking in all log output
- Studio dashboard restricted to localhost
- Secret files (`.env`) excluded from tool scanning
- License changed from MIT to Apache License 2.0
