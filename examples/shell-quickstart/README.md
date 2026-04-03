# Shell Scripts Quickstart Example

This directory shows how FolderMCP discovers shell scripts and exposes them
as MCP tools.

## What's Inside

```
shell-quickstart/
  scripts/
    deploy.sh      # Deploy to staging/production (destructive)
    health.sh      # Health-check a running service (read_only)
    backup.sh      # Back up a PostgreSQL database (side_effects)
```

Each script uses comment headers to declare tool metadata:

```bash
# tool: deploy
# description: Deploy the application to the target environment.
# risk: destructive
# param: environment: string: Target environment
```

## Getting Started

### 1. Initialize

```bash
foldermcp init examples/shell-quickstart
```

### 2. Review

```bash
cd examples/shell-quickstart
foldermcp review
```

### 3. Serve

```bash
foldermcp connect claude-desktop
foldermcp serve
```

## Risk Levels

| Script    | Risk         | Why                               |
|-----------|--------------|-----------------------------------|
| health.sh | read_only    | Only reads service status          |
| backup.sh | side_effects | Writes a file but is non-destructive |
| deploy.sh | destructive  | Mutates running infrastructure     |

FolderMCP enforces confirmation prompts for tools with elevated risk when
running in production mode.

## Next Steps

- Mix shell scripts with Python tools and OpenAPI specs in one workspace.
- Run `foldermcp test deploy -- staging v1.2.3` to try a tool locally.
- See the main [README](../../README.md) for the full CLI reference.
