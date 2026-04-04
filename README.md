# FolderMCP

[![CI](https://github.com/gtm-k/foldermcp/actions/workflows/ci.yml/badge.svg)](https://github.com/gtm-k/foldermcp/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8.svg?logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![MCP](https://img.shields.io/badge/MCP-2025--11--25-green.svg)](https://modelcontextprotocol.io)

Turn any folder of scripts into a secure, plug-and-play AI tool server.

FolderMCP scans directories containing Python, TypeScript/JavaScript, OpenAPI specs,
shell scripts, and documents (PDFs, images, CSVs) then serves them as
[MCP (Model Context Protocol)](https://modelcontextprotocol.io) tools and resources.
It handles discovery, introspection, dependency management, sandboxed execution,
and protocol translation automatically.

## Why FolderMCP?

- **Zero boilerplate.** Drop a Python, TypeScript, OpenAPI, or shell script into a folder and it becomes an MCP tool. PDFs and images become MCP resources. No SDK, no wrapper code, no manifest to maintain.
- **Secure by default.** Every tool runs inside a sandbox with deny-by-default permissions. Risk levels and human-in-the-loop approval are built in.
- **One command to connect.** `foldermcp connect claude-desktop` wires your tools into Claude Desktop (or any MCP-compatible client) in seconds.
- **Production-ready workflow.** Three review modes (dev, team, production), audit logging, and dependency isolation let you go from prototype to deployment safely.

## Quick Start

```bash
# 1. Install
go install github.com/foldermcp/foldermcp/cmd/foldermcp@latest

# 2. Initialize a workspace
foldermcp init ./my-tools

# 3. Review and approve discovered tools
cd my-tools
foldermcp review

# 4. Connect to Claude Desktop
foldermcp connect claude-desktop

# 5. Start the MCP server
foldermcp serve
```

See [examples/python-quickstart](examples/python-quickstart/) for a working
example with several sample tools.

## CLI Reference

| Command | Description |
|---------|-------------|
| `foldermcp init [path]` | Initialize a directory as a FolderMCP workspace |
| `foldermcp review` | Review and approve or disable discovered tools |
| `foldermcp serve` | Start the MCP server on stdin/stdout |
| `foldermcp connect <client>` | Configure a client to use this server |
| `foldermcp catalog` | List all discovered tools in a table |
| `foldermcp status` | Show summary of tool and dependency states |
| `foldermcp test <tool-name>` | Test a tool by running it locally |
| `foldermcp test --all` | Smoke-test all enabled tools |
| `foldermcp test --force` | Bypass state check during testing |
| `foldermcp review --approve-all` | Approve all pending tools |
| `foldermcp review --confirm=<tools>` | Set tools to requires_confirmation |
| `foldermcp doctor` | Check environment for common issues |
| `foldermcp deploy <target>` | Generate deployment artifacts |
| `foldermcp connect cursor` | Configure Cursor MCP client |
| `foldermcp ui` | Open developer studio |

### Shell Completion

FolderMCP supports shell completion for bash, zsh, fish, and PowerShell:

    foldermcp completion bash > /etc/bash_completion.d/foldermcp
    foldermcp completion zsh > "${fpath[1]}/_foldermcp"
    foldermcp completion fish > ~/.config/fish/completions/foldermcp.fish
    foldermcp completion powershell | Out-String | Invoke-Expression

### HTTP Endpoints (Team/Production Mode)

When running in team/production mode (HTTP transport):
- `/healthz` — health check endpoint
- `/readyz` — readiness check endpoint
- `/metrics` — Prometheus-compatible metrics

## Features

- **Auto-discovery** -- Scans directories for Python, TypeScript/JavaScript, OpenAPI specs, and shell scripts. Extracts tool metadata from type hints, docstrings, JSDoc, and schemas. PDFs, images, CSVs, and Markdown are discovered as MCP resources.
- **Pluggable introspectors** -- Python, TypeScript/JS, OpenAPI, and shell introspectors ship built-in. The plugin interface (`IntrospectorPlugin`) supports community-contributed languages.
- **Risk classification** -- Each tool is auto-tagged as `read_only`, `side_effects`, `destructive`, or `network` based on function names and HTTP methods. High-risk tools can require human confirmation.
- **Three review modes** -- `dev` (bulk approve), `team` (risk-based batching), and `production` (individual review per tool with audit trail).
- **NAS/shared drive support** -- Works on SMB, NFS, Azure Files, and cloud-mounted storage. Splits shared config (NAS) from local state (per-user). Auto-detects network filesystems.
- **Sandboxed execution** -- Tools run in isolated subprocesses with configurable timeouts, output limits, rate limiting, and path traversal protection.
- **Secret management** -- Auto-loads `.env` files for tool execution. Secrets never touch shared storage. Output sanitization redacts AWS keys, GitHub tokens, API keys, and private keys.
- **Audit logging** -- Every invocation is logged as structured JSON with rotation. Supports `foldermcp logs` and `--json` output for CI/CD.
- **Dependency management** -- Detects and installs Python (via uv) and Node.js dependencies into isolated environments with lockfile support.
- **Client integration** -- One-command setup for Claude Desktop, Claude Code, Cursor, VS Code, and Windsurf. Connection snippet generator for other clients.
- **Developer Studio** -- Local web dashboard (`foldermcp ui`) with live tool catalog, audit log, and status monitoring.
- **Deployment** -- Generate Docker, Cloud Run, and A2A agent-card.json artifacts with `--dry-run` preview.
- **Watch mode** -- `foldermcp serve --watch` auto-reloads on file changes (polling-based, works on NAS).
- **Configuration via YAML** -- All settings live in `foldermcp.yaml` with sensible defaults, schema versioning, and tool profiles.

## Configuration

FolderMCP is configured through a `foldermcp.yaml` file in the workspace root.
Run `foldermcp init` to generate one with defaults, or copy the
[foldermcp.yaml.example](foldermcp.yaml.example) template.

Key sections:

- **scan** -- Include/exclude glob patterns for file discovery.
- **tools** -- Per-tool state overrides, descriptions, and risk levels.
- **dependencies** -- Python and Node.js packages to install.
- **tool_routing** -- Controls how many tools are exposed per context and which routing strategy to use.

## Security

FolderMCP follows a deny-by-default security model. All tools start in a
`pending` state and must be explicitly approved before they can be invoked.
Execution is sandboxed, and all invocations are audit-logged.

For details on reporting vulnerabilities and the full security model, see
[SECURITY.md](SECURITY.md).

## Project Structure

```
cmd/foldermcp/       CLI entry point (15 commands)
internal/
  audit/             Structured JSON audit logging with rotation
  cache/             Content-addressed source file cache
  config/            YAML configuration with schema versioning
  deps/              Dependency manager (uv for Python, npm for JS)
  export/            A2A agent-card.json export
  introspect/        Tool introspectors (Python, TypeScript/JS, OpenAPI, Shell, Resources)
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

## License

[Apache License 2.0](LICENSE)
