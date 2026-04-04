# FolderMCP

[![CI](https://github.com/gtm-k/foldermcp/actions/workflows/ci.yml/badge.svg)](https://github.com/gtm-k/foldermcp/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8.svg?logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![MCP](https://img.shields.io/badge/MCP-2025--11--25-green.svg)](https://modelcontextprotocol.io)

**Turn any folder into a secure MCP tool server.**

FolderMCP scans directories containing Python, TypeScript/JavaScript, OpenAPI specs,
shell scripts, and documents (PDFs, images, CSVs, Markdown), then serves them as
[MCP (Model Context Protocol)](https://modelcontextprotocol.io) tools and resources.
It handles discovery, introspection, dependency management, sandboxed execution, and
protocol translation automatically -- no SDK, no wrapper code, no manifest to maintain.

## Why FolderMCP?

- **Zero boilerplate for all languages.** Drop a Python function, TypeScript export, OpenAPI spec, or shell script into a folder and it becomes an MCP tool. PDFs, images, and CSVs become MCP resources. No SDK integration required.
- **Secure by default.** Every tool starts in a `pending` state with deny-by-default permissions. Execution is sandboxed with configurable timeouts, output limits, and secret redaction.
- **NAS and shared drive compatible.** Works on SMB, NFS, Azure Files, and cloud-mounted storage. Auto-detects network filesystems and splits shared config from local state.
- **One command to connect.** `foldermcp connect <client>` wires your tools into Claude Desktop, Claude Code, Cursor, VS Code, or Windsurf in seconds.
- **Production-ready workflow.** Three review modes (dev, team, production), structured audit logging, Docker/Cloud Run deployment, and health/metrics endpoints.

## Quick Start

```bash
# 1. Install
go install github.com/gtm-k/foldermcp/cmd/foldermcp@latest

# 2. Initialize
foldermcp init ./my-tools

# 3. Review & approve
foldermcp review --approve-all

# 4. Connect to your AI client
foldermcp connect claude-desktop

# 5. Start serving
foldermcp serve
```

## Supported Formats

| Format | Extensions | What's Discovered | Example |
|--------|-----------|-------------------|---------|
| Python | `.py` | Functions with type hints | `def query(sql: str) -> str` |
| TypeScript/JS | `.ts`, `.js`, `.mjs`, `.cjs` | Exported functions | `export function analyze(data: string)` |
| OpenAPI | `.yaml`, `.json` | API operations | GET/POST/DELETE endpoints |
| Shell | `.sh`, `.bash` | Script wrappers | `./deploy.sh` |
| Documents | `.pdf`, `.md`, `.txt`, `.csv` | MCP Resources | Context docs for AI agents |
| Images | `.png`, `.jpg`, `.svg` | MCP Resources | Diagrams, screenshots |

## CLI Reference

| Command | Description | Key Flags |
|---------|-------------|-----------|
| `foldermcp init [path]` | Initialize a directory as a workspace | `--template` (python, openapi, shell) |
| `foldermcp review` | Review and approve/disable discovered tools | `--approve-all`, `--confirm`, `--disable`, `--mode`, `--dry-run` |
| `foldermcp serve` | Start the MCP server | `--transport` (stdio, http), `--mode`, `--port`, `--watch`, `--profile` |
| `foldermcp connect <client>` | Configure a client (claude-desktop, claude-code, cursor, vscode, windsurf) | `--snippet`, `--mode` |
| `foldermcp catalog` | List all discovered tools in a table | `--state`, `--risk`, `--type` |
| `foldermcp status` | Show tool and dependency state summary | `--json` |
| `foldermcp test [tool]` | Test a tool by running it locally | `--all`, `--force`, `--params` |
| `foldermcp diff` | Show what would change on re-scan | `--json` |
| `foldermcp doctor` | Check environment for issues | `--fix` |
| `foldermcp deploy <target>` | Generate deployment artifacts (docker, cloudrun) | `--dry-run` |
| `foldermcp export a2a` | Export A2A agent-card.json | `--name`, `--url`, `--version` |
| `foldermcp logs` | View the audit log | `--follow`, `--json` |
| `foldermcp ui` | Open Developer Studio dashboard | `--port` (default 3001) |
| `foldermcp completion` | Generate shell completions (bash, zsh, fish, powershell) | |

All commands support the `--json` global flag for machine-readable output.

## NAS / Shared Drive Support

FolderMCP is designed to work on network-attached storage out of the box:

- **Supported filesystems:** SMB, NFS, Azure Files, cloud-mounted storage (Google Drive, OneDrive).
- **Auto-detection:** Detects network filesystem type at runtime and adapts behavior accordingly.
- **Split storage:** Shared configuration (`foldermcp.yaml`, tool metadata) lives on the NAS; local state (audit logs, caches) lives per-user on the local machine.
- **Shared approvals:** Tool approval state is stored on the shared drive so the whole team sees the same review status.
- **Watch mode:** `foldermcp serve --watch` uses polling-based file watching, which works reliably on network filesystems where inotify/FSEvents are unavailable.

## HTTP Endpoints (Team/Production Mode)

When running with `--transport http` in team or production mode:

- `/healthz` -- health check endpoint
- `/readyz` -- readiness check endpoint
- `/metrics` -- Prometheus-compatible metrics

Supports API key authentication and self-signed TLS certificate generation.

## Developer Studio

```bash
foldermcp ui
```

Opens a local web dashboard at `localhost:3001` with:

- Live tool catalog with state, risk level, and descriptions
- Audit log viewer with filtering
- Server status and health monitoring

## Configuration

All settings live in `foldermcp.yaml` at the workspace root. Run `foldermcp init` to generate one with defaults.

```yaml
version: 1

scan:
  include: ["*.py", "*.ts", "*.js", "*.yaml", "*.yml", "*.sh"]
  exclude: ["tests/**", "node_modules/**", ".git/**"]

tools:
  # Per-tool overrides
  # my_tool:
  #   state: "enabled"
  #   description: "Custom description"
  #   risk: "high"

dependencies:
  python: []   # e.g., [requests, flask]
  node: []     # e.g., [express, typescript]

tool_routing:
  max_tools_per_context: 20
  strategy: "profile"
  profiles:
    # read_only: [query_db, list_files]
    # admin: [delete_records, deploy_to_prod]
```

## Security

FolderMCP follows a deny-by-default security model:

- All tools start in `pending` state and must be explicitly approved before invocation.
- Execution is sandboxed in isolated subprocesses with configurable timeouts and output limits.
- Output sanitization automatically redacts AWS keys, GitHub tokens, API keys, and private keys.
- Per-tool risk labeling: `read_only`, `side_effects`, `destructive`, `network`.
- Structured audit logging with JSON rotation for every invocation.

For details on reporting vulnerabilities and the full security model, see [SECURITY.md](SECURITY.md).

## Project Structure

```
cmd/foldermcp/       CLI entry point (15 commands)
internal/
  audit/             Structured JSON audit logging with rotation
  cache/             Content-addressed source file cache
  config/            YAML configuration with schema versioning
  deps/              Dependency manager (uv for Python, npm for JS)
  export/            A2A agent-card.json export
  introspect/        Tool introspectors (Python, TS/JS, OpenAPI, Shell, Resources)
  lifecycle/         State transition validation
  pythonrt/          Shared Python runtime detection
  sandbox/           Sandboxed executor with rate limiting and path guard
  server/            MCP server (stdio + HTTP), auth, TLS, health/metrics
  state/             SQLite state store
  studio/            Developer web dashboard
  watcher/           Polling file watcher (NAS-compatible)
  workspace/         NAS split-storage manager, FS detection, shared approvals
examples/            Sample projects (Python, OpenAPI, Shell)
```

## Contributing

Contributions are welcome. Please open an issue to discuss non-trivial changes
before submitting a pull request.

1. Fork the repository.
2. Create a feature branch from `main`.
3. Add tests for new functionality.
4. Run `make test` and `make lint` before submitting.
5. Open a pull request with a clear description of the change.

See [CONTRIBUTING.md](CONTRIBUTING.md) for full guidelines.

## License

[Apache License 2.0](LICENSE)
