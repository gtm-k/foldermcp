# Python Quickstart Example

This directory contains a minimal set of Python tools for trying out FolderMCP.
It includes simple tools for database queries, text formatting, and weather lookups.

## Directory Structure

```
python-quickstart/
  greet.py            # Simple greeting tool (root-level)
  add.py              # Addition tool (root-level)
  tools/
    query_db.py       # Read-only SQL query tool
    format_text.py    # Markdown formatting + word count
    fetch_weather.py  # Weather lookup tool
```

## Getting Started

### 1. Initialize the workspace

```bash
foldermcp init examples/python-quickstart
```

This scans the directory, discovers all Python tools, and creates a `foldermcp.yaml`
configuration file.

### 2. Review discovered tools

```bash
cd examples/python-quickstart
foldermcp review
```

In dev mode you will be prompted to approve all discovered tools at once.
For production use, pass `--mode=production` to review each tool individually.

### 3. Connect to a client

```bash
foldermcp connect claude-desktop
```

This registers the FolderMCP server in Claude Desktop's configuration so it can
discover and call your tools automatically.

### 4. Start the MCP server

```bash
foldermcp serve
```

The server starts on stdin/stdout using the MCP stdio transport. Only enabled
tools are exposed to the connected client.

## Tool Anatomy

Each Python file uses a docstring header to declare tool metadata:

```python
"""A weather lookup tool.

tool: get_weather
description: Get the current weather for a city.
risk: read_only
param: city: string: The city to get weather for
"""

def get_weather(city: str) -> str:
    """Get the current weather for a city."""
    return f"Weather in {city}: 72F, Sunny"
```

FolderMCP reads the docstring at scan time to extract the tool name, description,
risk level, and parameter schema. The function body runs inside a sandbox at
invocation time.

## Next Steps

- Run `foldermcp catalog` to see all tools and their states.
- Run `foldermcp test <tool-name>` to invoke a tool locally.
- Run `foldermcp doctor` to check your environment for common issues.
- See the main [README](../../README.md) for the full CLI reference.
