# Research Notes — Local-First Search Systems

## The Problem of NAS Search

Network-attached storage devices have become ubiquitous in home and small
office environments. The typical Synology DS923+ or QNAP TS-464 holds
between 1 TB and 100 TB of diverse data: code repositories, research papers,
family photos, media collections, and miscellaneous documents accumulated
over years.

Built-in search on these devices is universally poor:
- Synology Drive search takes 6+ seconds and frequently returns nothing
- QNAP QuMagie is photo-focused and ignores text content
- Windows SMB search has documented false-negative rates on large shares
- macOS Spotlight indexing over SMB is unreliable

## What Would Good Look Like?

A good local search system for personal NAS storage should:

1. **Index everything** — code, PDFs, markdown, photos (EXIF), media (metadata)
2. **Understand structure** — AST for code, heading hierarchy for docs
3. **Run locally** — no cloud dependency, no data leaving the network
4. **Handle scale** — 10 GB in minutes, 1 TB in hours, not days
5. **Support Claude/LLM access** — via MCP tools, so AI assistants can search

## Existing Approaches

### DEVONthink

Mac-only, $200. Uses a proprietary AI classifier trained on the user's
corpus. Excellent for researchers but platform-locked and expensive.
No MCP integration. No NAS-native deployment.

### Recoll

Open-source full-text search. Linux-native. Good for FTS but no semantic
search, no AST awareness, no MCP integration. UI is dated.

### Meilisearch / Typesense

Cloud-first search engines. Excellent API ergonomics but designed for
web applications, not personal archives. Would require significant
adaptation for NAS deployment and file-based indexing.

## The foldermcp Approach

foldermcp v3.0 takes a different approach:

1. **AST-first extraction** using tree-sitter for code understanding
2. **Layered retrieval**: FTS5 keyword → vector similarity → graph traversal
3. **Local ONNX embeddings** (all-MiniLM-L6-v2) for semantic search
4. **MCP tools** for direct Claude integration
5. **SQLite storage** for single-file, crash-recoverable persistence
6. **Hardware-aware** with tiered performance profiles for NAS hardware

The key insight is that most personal archives have strong structure that
generic search engines ignore: Python files have functions and classes,
research papers have abstracts and citations, Obsidian vaults have
wikilinks and tags. Exploiting this structure gives better retrieval than
pure text search, without the cost of LLM-at-index-time approaches.
