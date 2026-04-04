# FolderMCP

Turn any folder of scripts into a secure, plug-and-play AI tool server.

FolderMCP scans directories containing Python scripts and OpenAPI specifications,
then serves them as [MCP (Model Context Protocol)](https://modelcontextprotocol.io)
tools. It handles discovery, introspection, dependency management, sandboxed
execution, and protocol translation automatically.

## Why FolderMCP?

- **Zero boilerplate.** Drop a Python file into a folder and it becomes an MCP tool. No SDK, no wrapper code, no manifest to maintain.
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

- **Auto-discovery** -- Scans directories for Python scripts and OpenAPI specs, extracts tool metadata from docstrings and schemas.
- **Pluggable introspectors** -- Python and OpenAPI introspectors ship built-in; the plugin interface supports additional languages.
- **Risk classification** -- Each tool is tagged as `read_only`, `state_changing`, or `destructive`. High-risk tools can require human confirmation.
- **Three review modes** -- `dev` (bulk approve), `team` (shared review), and `production` (individual review per tool).
- **Sandboxed execution** -- Tools run in isolated subprocesses with configurable timeouts and output limits.
- **Output sanitization** -- Results are truncated, validated, and scrubbed before being returned to the client.
- **Audit logging** -- Every tool invocation is logged with timestamp, tool name, parameters, and result status.
- **Dependency management** -- Detects and installs Python and Node.js dependencies into isolated environments.
- **Client integration** -- One-command setup for Claude Desktop; extensible to other MCP clients.
- **SQLite state store** -- Tracks tool states, metadata, and configuration in a local `.foldermcp/` directory.
- **Configuration via YAML** -- All settings live in `foldermcp.yaml` with sensible defaults and schema versioning.

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
cmd/foldermcp/       CLI entry point and commands
internal/
  audit/             Audit logging
  config/            YAML configuration loader
  deps/              Dependency manager
  introspect/        Tool introspectors (Python, OpenAPI)
  sandbox/           Sandboxed executor and output sanitizer
  server/            MCP server (stdio transport)
  state/             SQLite state store
examples/            Sample tool directories
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

[MIT](LICENSE)
