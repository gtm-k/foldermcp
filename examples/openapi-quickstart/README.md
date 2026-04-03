# OpenAPI Quickstart Example

This directory contains a minimal OpenAPI 3.0 specification that FolderMCP
can introspect to produce MCP tools automatically.

## What's Inside

- `api.yaml` -- A user management API with four operations:
  - `listUsers` (GET /users)
  - `createUser` (POST /users)
  - `getUser` (GET /users/{userId})
  - `deleteUser` (DELETE /users/{userId})

## Getting Started

### 1. Initialize the workspace

```bash
foldermcp init examples/openapi-quickstart
```

FolderMCP parses `api.yaml`, discovers each operation, and registers it as
a tool.

### 2. Review and approve

```bash
cd examples/openapi-quickstart
foldermcp review
```

### 3. Connect and serve

```bash
foldermcp connect claude-desktop
foldermcp serve
```

## How It Works

FolderMCP reads the OpenAPI spec with the built-in OpenAPI introspector.
Each `operationId` becomes a tool, with parameters derived from path, query,
and request body schemas. Risk levels are inferred from the HTTP method:

| Method | Default Risk  |
|--------|---------------|
| GET    | read_only     |
| POST   | side_effects  |
| PUT    | side_effects  |
| DELETE | destructive   |

You can override these in `foldermcp.yaml` after init.

## Next Steps

- Combine OpenAPI specs with Python scripts in the same workspace.
- Run `foldermcp catalog` to see discovered tools.
- See the main [README](../../README.md) for the full CLI reference.
