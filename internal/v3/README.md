# `internal/v3/` — FolderMCP v3.0 M1 walking skeleton

This subtree hosts the v3.0 rewrite per the locked plan dated 2026-04-11
(`gtm-k/prd/foldermcp/plans/2026-04-11-foldermcp-v3.0-M1-implementation-plan.md`,
commit `6568063` in the private prd-repo).

v3.0 is a split-daemon architecture: a long-running cgo-required indexer
(`foldermcp index-v3`) writes to a SQLite + FTS5 + `sqlite-vec` store, and a
cgo-free MCP query server (`foldermcp serve-v3`) reads from it over gRPC. A
stdio shim (`foldermcp mcp-v3`) fronts Claude Desktop. The v0.1.0 packages in
`internal/` are untouched during M1 and continue to serve existing users until
the v3.0 cutover.

During M1 the v3 cobra subcommands use a transitional `-v3` suffix to avoid
colliding with v0.1.0's existing commands. At v3.0 cutover the suffix will be
dropped from `Use:` strings and kept as `Aliases` for muscle memory.

Package layout mirrors the plan's §"File structure" section. Build gates and
scope fence live in the plan's §"Explicitly NOT in M1" section — re-read that
at the start of every execution session before touching code.
