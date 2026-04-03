# PRD v2.0 — FolderMCP: Plug & Play AI Protocol Runtime

**Status:** Approved for Execution  
**Version:** 2.0 (Revised from v1.0)  
**Date:** April 2026  
**Owner:** Product / Founding Team  
**Classification:** Internal — Product Strategy

***

## Executive Summary

Developers are racing to connect their code, scripts, and APIs to AI agents — but every integration still demands bespoke configuration, boilerplate servers, JSON-RPC wiring, and fragile auth setup. Agent interoperability has simultaneously fractured into at least seven competing protocols (MCP, A2A, ACP, AGNTCY, ANP, NLIP, Open Floor), creating an overwhelming landscape where building support for even one protocol correctly takes skilled engineers a full day.[^1][^2]

**FolderMCP** is an open-core, zero-configuration runtime. Point it at any directory containing scripts, OpenAPI specs, or binaries — and it converts the folder into a secure, deployable, MCP-compatible AI tool surface in minutes. No boilerplate. No YAML. No DevOps.

### What Changed in v2.0

v1.0 set the right vision but was too broad for a successful v1 ship. The three critical strategic corrections made in this revision:

1. **Narrowed wedge:** MCP-first rather than all protocols simultaneously. A2A, ACP, and AGNTCY become compatibility metadata outputs before becoming full runtime targets.
2. **Approval layer added:** "Discover automatically, expose intentionally." Tools are never auto-published. An explicit review step separates discovery from exposure.
3. **Practical blockers fixed:** Auto-dependency resolution (so scripts with real-world imports actually run), a local developer studio for debugging, and a `doctor` command for self-diagnosis.

### One-Line Pitch

> **FolderMCP turns existing code, scripts, and APIs into secure MCP tools in minutes — with review, control, and production-ready deployment built in.**

***

## 1. Problem Statement

### 1.1 The Setup Tax

Despite MCP reaching 97 million monthly SDK downloads and 10,000+ active public servers, the developer experience of actually deploying an MCP server remains painful. Real practitioners describe the process as "confusing, inconsistent, and far from intuitive". Documented failure modes include:[^3][^1]

- Cold start delays of up to 5 seconds in serverless environments[^1]
- No standard Infrastructure-as-Code template — engineers figure out all components manually[^1]
- Logging incompatibilities between FastAPI and FastMCP that break standard monitoring[^1]
- Dependency conflicts when running multiple MCP servers simultaneously[^4]
- Full reconfiguration required when switching development environments[^4]
- Average **24 hours** to build a basic MCP server from scratch for a new data source[^2]

Even VSCode characterized the original installation experience as "Byzantine" — users had to manually copy JSON configuration blobs and hardcode API keys. VSCode built a registry-based install UX to solve this for their IDE context, but the underlying infrastructure problem remains unsolved for general deployment.[^5]

### 1.2 The Security Default Crisis

The MCP ecosystem has a systemic security problem that is getting worse, not better. Key data points from 2025-2026:

- A community audit found **41% of servers** listed in the official MCP Registry have zero authentication[^6]
- Security researchers filed over **30 CVEs** targeting MCP servers, clients, and infrastructure between January–February 2026[^7]
- CVE-2025-6514, a command injection vulnerability in the widely-used `mcp-remote` package (437,000+ downloads at time of disclosure), carried a CVSS score of 9.6[^7]
- OWASP published a formal MCP Top 10 covering: token mismanagement, privilege escalation, tool poisoning, supply chain attacks, command injection, insufficient auth, weak auditing, shadow MCP servers, and context injection[^8]
- Teleport's 2026 survey found **67%** of infrastructure and security leaders still rely on static credentials for AI systems[^9]
- Over-privileged AI systems drive a **4.5x higher incident rate** than least-privileged deployments[^9]

FastMCP, the most widely used framework, ships with no authentication and no TLS by default. Connecting to an MCP server built with the base spec grants access to every tool on that server — there is no per-tool authorization in the core protocol.[^10][^9]

### 1.3 The Dependency & Runtime Gap

"Zero-config" claims in existing tools break the moment a real-world script imports a package like `pandas`, `requests`, or `boto3`. No current MCP deployment tool handles environment isolation and dependency resolution automatically. This is a practical blocker that silently kills first-run success rates.

### 1.4 Enterprise Deployment Incompatibility

Over 95% of Fortune 500 companies operate serverless architectures — primarily Azure Functions and AWS Lambda. MCP's default Docker-packaged server model is fundamentally incompatible. As Nayan Paul, Chief Azure Architect at Accenture, wrote: "Unless MCP evolves to support serverless deployment options, I'll likely keep building around it instead of inside it".[^1]

The MCP spec explicitly defers enterprise security to extensions with no dedicated working groups. Gaps include: no mTLS, no per-tool authorization, no audit trail specification, and no gateway behavior specification in the core spec. Five of the OWASP MCP Top 10 risks live directly in these gaps.[^8][^9]

### 1.5 Tool Overload Collapse

When connecting just four common enterprise MCP servers (Redis, GitHub, Jira, Grafana), the combined tool inventory reaches 167 tools consuming ~60,000 tokens before the user speaks a single word. In production environments, 150,000+ token tool contexts are common. Beyond a threshold, adding more tools makes agents measurably worse — tool selection accuracy drops, response times increase, and costs compound.[^11]

### 1.6 The Debugging Black Box

When an agent fails to use a tool correctly, developers currently have no way to inspect the exact JSON-RPC payload sent by the LLM versus what the tool expected. This debugging blindspot kills iteration speed and erodes developer trust in the entire system.

### 1.7 Fragmentation at Scale

- **64%** of firms operate with siloed AI tech stacks and duplicate tooling[^12]
- **70%** of AI scaling failures stem directly from poor interoperability[^12]
- **49%** of development teams use more than five separate AI tools[^13]
- **90%** of developers lose 6+ hours per week to non-coding inefficiencies, with tool fragmentation cited as a top cause[^14]

### Problem in One Sentence

> "Teams already have useful tools — exposing them as secure, debuggable, manageable MCP servers is still too much work."

***

## 2. Why This Product Should Exist (Market Validation)

MCP is no longer speculative. Adoption signals are unambiguous:

- **89%** of new enterprise AI agent projects plan MCP integration[^2]
- **73%** of enterprise developers cite MCP as their preferred agent-tool connectivity standard[^2]
- **400%** month-over-month growth in MCP server registrations during peak Q1 2026[^2]
- **12** Fortune 500 companies have published proprietary MCP servers[^2]
- The AI agents market grows from $7.84B (2025) to $52.62B by 2030 at a 46.3% CAGR[^15]
- Developer tools with active open-source components achieve **45% faster enterprise adoption** than proprietary alternatives[^16]

At the same time, enterprise readiness is incomplete. The official MCP roadmap still lists horizontal scaling, stateless operation, and middleware patterns as active gaps. The spec explicitly defers most enterprise security requirements to extensions not yet shipped. This creates a window for infrastructure products that make MCP deployable in the real world now — not in a future spec version.[^17][^9]

The competitive gap is also clear. Discovery and catalog products are already saturated (Glama: 6,000+ listings, mcp.so: 5,000+, Smithery: 2,000+, Docker MCP Catalog: 300+ verified servers). What does not yet exist is a clean **"bring your own folder, expose it safely, run it anywhere"** deployment path for private and custom tools. That is the unoccupied position FolderMCP should own.[^18][^6]

***

## 3. Target Users

### Primary ICP: Platform & AI Infrastructure Teams

Teams inside startups and enterprises responsible for making internal APIs, scripts, and data operations safely available to AI products, copilots, and agents. They need policy, auth, deployment, and observability from day one.

**Characteristics:** Already know MCP exists. Blocked by auth complexity, enterprise deployment mismatch, and lack of governance tooling. Have budget for infrastructure products.

### Secondary ICP: Product Engineers Building AI Features

Developers building internal assistants, coding tools, workflow agents, and custom automations who need to connect LLM-powered workflows to existing business operations — without learning the full MCP operational stack.

**Characteristics:** Time-constrained. Will adopt tools with excellent DX. Strong word-of-mouth vector.

### Tertiary ICP: SaaS Vendors with Existing APIs

SaaS companies wanting to expose their product as an AI-accessible tool by wrapping existing OpenAPI specs into an MCP-compatible interface. Want to appear in AI client tool catalogs without a full integration team.

**Characteristics:** Will pay for managed hosting and registry publishing. Strong growth vector for FolderMCP's ecosystem play.

***

## 4. Jobs To Be Done

**Core JTBD:**
- When a team has useful code or APIs already working, help them expose those capabilities to AI clients quickly and safely, without building custom integration infrastructure.
- When deploying to production, provide safer auth, policy, audit, and cloud deployment defaults than the ecosystem standard.
- When a tool catalog grows large, help agents discover only the relevant tools instead of receiving every tool in context.

**User Stories:**
- As a platform engineer, I want to point FolderMCP at an internal repo and get a secure MCP endpoint I can deploy behind company auth, without writing any MCP server code.
- As a product engineer, I want OpenAPI specs turned into MCP tools without hand-writing server boilerplate.
- As a security-conscious team, I want deny-by-default behavior, audit logs, and per-tool policies before any tool is callable.
- As an AI team, I want only the relevant subset of tools exposed per task to keep the LLM effective.
- As a solo developer, I want my local Python scripts callable from Claude Desktop in under 10 minutes.

***

## 5. Product Principles

1. **MCP-first, not protocol-maximalist** — Win the most urgent market segment before expanding.
2. **Discover automatically, expose intentionally** — Auto-discovery is a feature; auto-exposure is a risk.
3. **Secure by default** — The absence of auth should require explicit opt-in, never the default.
4. **Fast path to first value** — From `foldermcp init` to working MCP connection in under 10 minutes.
5. **Production credibility over demo magic** — Deployability, logs, policies, and diagnostics matter more than feature surface area.
6. **Make debugging visible** — Developers should see exactly what flows between the agent and their tools.
7. **Protocol expansion only after proven wedge** — A2A/ACP become real runtime targets once MCP adoption is proven.

***

## 6. What Changed from v1.0

| Dimension | v1.0 | v2.0 |
|---|---|---|
| Protocol scope | All 5 protocols simultaneously | MCP-first; A2A/ACP as compatibility metadata exports |
| Exposure model | Zero-config, instant publish | Discover automatically, approve before exposing |
| Security posture | Deny-all default | Deny-all + explicit tool-level approval flow |
| Dependency handling | Not addressed | Auto-isolation via `uv` (Python) / npm (JS) |
| Developer debugging | Not addressed | `foldermcp ui` local studio for payload inspection |
| Self-diagnosis | Not addressed | `foldermcp doctor` command |
| Competitor framing | Undefined | Positioned against Docker MCP Toolkit, Composio — differentiating on private/custom folder deployment |
| Primary buyer | Generic developers | Platform teams, API owners, AI infra engineers |
| Business model | Mentioned | Open-core with buyer-based segmentation defined |
| MVP scope | 12-month roadmap | 30-60-90 day execution plan with exit criteria |

***

## 7. Core Feature Specifications

### 7.1 Intelligent Folder Scanner

FolderMCP watches the target directory using filesystem events and maintains a live catalog of discoverable tools.

**Supported inputs:**

| Source Type | What's Detected | v1 | v2 |
|---|---|---|---|
| Python files | Functions, signatures, docstrings | ✅ | ✅ |
| TypeScript/JavaScript | Exported functions | ✅ | ✅ |
| OpenAPI specs | All operations, parameters, schemas | ✅ | ✅ |
| Shell scripts | Command-line tool wrapper | ✅ | ✅ |
| `agent-card.json` | A2A-compatible agent metadata | 🔶 | Compatibility export |
| `*.csv`, `*.parquet`, `*.json` | Data resources | 🔶 | Phase 2 |
| Dockerfile | Containerized service | 🔶 | Phase 2 |

**Behavior:**
- Deep scan on startup, live-reload on file changes (detected within 2 seconds)
- LLM-assisted description generation from docstrings and code comments (local, offline embedding model — no external API calls required)
- Auto-generates tool names, input schemas, and invocation wrappers
- Unsupported files are silently skipped with an optional verbose log

**Acceptance criteria:**
- A folder with 10 Python files is fully cataloged in under 10 seconds
- Generated tool descriptions pass MCP schema validation without manual editing for 85%+ of well-documented functions
- Users can override any auto-generated name, description, or schema via `foldermcp.yaml`

### 7.2 Auto-Dependency Resolution

This is a critical v2.0 addition. A Python tool that imports `pandas` fails silently in the runtime without environment isolation. FolderMCP eliminates this blocker.

**Behavior:**
- On scan, FolderMCP inspects `import` statements in Python files and `require`/`import` in TypeScript
- Automatically builds an isolated virtual environment per folder using `uv` (Python) and a pinned `node_modules` (JS/TS)
- If a `requirements.txt`, `pyproject.toml`, or `package.json` exists, it is used directly
- If not, dependencies are inferred from import statements and installed automatically
- Dependency isolation runs in the background — does not block `foldermcp serve`
- Environment is cached and only rebuilt when dependencies change

**Acceptance criteria:**
- A Python script importing `pandas`, `requests`, and `boto3` with no `requirements.txt` runs successfully on first invocation
- Cold start after dependency installation completes in under 30 seconds for typical scripts
- Conflicting dependencies across multiple tools are isolated per-tool, not globally

### 7.3 Review & Approval Layer

This is the most important new product surface in v2.0. The security reality of the MCP ecosystem — OWASP Top 10, 30+ CVEs, 41% of registry servers without auth — makes automatic tool publishing unacceptably risky. FolderMCP separates discovery from exposure.[^6][^7][^8]

**Behavior:**
- After a scan, all discovered tools are in `PENDING` state — visible in the catalog but NOT invocable
- `foldermcp review` presents a terminal UI showing:
  - Tool name and source file
  - Auto-generated description
  - Inferred input schema
  - Risk label (Read-only / Side-effects / Destructive / Network)
  - Default state recommendation
- Developer explicitly marks each tool as `ENABLED`, `DISABLED`, or `REQUIRES_CONFIRMATION`
- `REQUIRES_CONFIRMATION` tools prompt the connected AI client for user approval before each invocation
- In `--mode=production`, new tools discovered after initial approval default to `DISABLED` until re-reviewed

**`foldermcp.yaml` approval state example:**
```yaml
tools:
  query_database:
    state: enabled
    description: "Runs a read-only SQL query against the analytics DB"
    risk: read_only
  delete_records:
    state: requires_confirmation
    risk: destructive
  deploy_to_prod:
    state: disabled
```

**Acceptance criteria:**
- No tool is invocable until explicitly approved, even in dev mode
- The review flow completes in under 5 minutes for a folder with 20 discovered tools
- Risk labels are accurate for 90%+ of standard Python/OpenAPI tools
- Approval state persists across restarts

### 7.4 Secure Multi-Mode Runtime

FolderMCP inverts the ecosystem's insecure defaults. Security should be the zero-config path, not the opt-in.

**Runtime modes:**

| Mode | Auth | TLS | Tools Default | Audit Log | Use Case |
|---|---|---|---|---|---|
| `dev` | None required | Self-signed | Approved tools only | Local file | Local iteration |
| `team` | API key required | TLS enforced | Deny-all for new tools | Structured JSON | Shared team server |
| `production` | OAuth2/PKCE or mTLS | TLS enforced | Deny-all | OTLP export | Enterprise deployment |

**Security capabilities:**
- **Per-tool authorization:** Connecting to the server does NOT grant access to all tools. Each tool has its own policy state[^9]
- **Execution sandboxing:** Python tools run in restricted subprocess workers with configurable memory/CPU/timeout limits; shell tools run with restricted syscall profiles
- **Secret injection:** Secrets are injected as environment variables at invocation time; never written to disk or logged
- **Audit log fields:** Tool name, invocation parameters, calling identity, authorization decision, timestamp, session context
- **Signed manifests:** FolderMCP generates an HMAC signature of the tool catalog at startup; drift alerts on supply chain changes[^19][^20]

### 7.5 MCP Server Runtime

FolderMCP implements the MCP spec version 2025-11-25 with full support for:[^21]

- **Transports:** stdio (local), Streamable HTTP (remote) — SSE deprecated per spec direction[^22]
- **Primitives:** Tools, Resources, Prompts (Phase 2), Tasks/async (Phase 2)
- **Discovery:** Auto-publishes `/.well-known/mcp/server-card.json` for auto-discovery by Claude Desktop, ChatGPT, VS Code, Cursor, and other MCP clients[^3]
- **Client compatibility:** Claude Desktop, Cursor, VS Code Copilot, Windsurf, Continue.dev, Goose, and all MCP-spec-compliant clients[^23]

**Acceptance criteria:**
- All MCP conformance tests pass before v1 release
- Local startup in under 10 seconds for typical projects
- A connected client can list, inspect, and invoke all approved tools

### 7.6 Semantic Tool Routing

Built-in tool routing prevents the token explosion that collapses agent performance at scale.[^11]

**v1 approach (rule-based):** Profiles and namespaces for grouped tool sets, with a configurable maximum tool count per session. Fast to ship, easy to debug.

**v2 approach (semantic):** Local vector index of tool descriptions; tools retrieved by semantic similarity to the incoming query rather than dumped wholesale.

**Configuration:**
```yaml
tool_routing:
  max_tools_per_context: 20
  strategy: semantic         # or: profile, namespace, all
  embedding_model: "all-MiniLM-L6-v2"   # local, offline
  profiles:
    read_only: [query_db, list_files, search_docs]
    billing: [charge_card, refund, view_invoice]
    dev_tools: [run_tests, lint, format_code]
```

**Acceptance criteria:**
- A folder with 100 tools exposes a maximum of 20 to any single agent context by default
- Profile switching applies without restarting the server
- Semantic routing accuracy validated against a test set of agent queries before v2 release

### 7.7 Developer Observability Studio (`foldermcp ui`)

When an agent fails to use a tool, developers have no visibility into the exact JSON-RPC payload exchanged. `foldermcp ui` solves this with a local web dashboard.

**Features:**
- **Live invocation log:** See every tool call — exact JSON payload the LLM sent, exact response returned, latency, status
- **Token budget dashboard:** See how much context your tool descriptions consume per session; warnings when approaching typical context window limits
- **Manual test harness:** A built-in interface to invoke any tool manually with custom parameters, independent of any AI client
- **Tool catalog browser:** Browse all discovered tools, their schemas, approval states, and description quality scores
- **Dependency status panel:** See which virtual environments are built, which tools have unresolved dependencies

**Architecture:**
- Lightweight embedded web server (runs as a sidecar to `foldermcp serve`)
- Accessible at `http://localhost:3001` when active
- Read-only by default — cannot change approval states (requires CLI for state changes)
- Zero external dependencies; offline-first

**Acceptance criteria:**
- `foldermcp ui` starts in under 3 seconds
- Live invocation log shows payloads within 500ms of an actual tool call
- Token count estimates are within 10% of actual model tokenizer counts

### 7.8 `foldermcp doctor` Diagnostics

Given that setup confusion is the #1 documented pain point, a self-diagnostic command reduces support burden and increases first-run success rates.[^5][^1]

**What `doctor` checks:**
- Runtime dependencies present (Python/Node versions, uv, Docker)
- All approved tools have working dependency environments
- MCP server is reachable from localhost
- Auth configuration is valid (for non-dev modes)
- At least one MCP client is configured to connect (detects Claude Desktop config, VSCode settings)
- No port conflicts
- TLS certificate validity (for team/production modes)
- Tool descriptions pass MCP schema validation
- Known dangerous configuration patterns (e.g., production mode with no auth)

**Output format:**
```
foldermcp doctor

✅ Runtime: Python 3.12, Node 22, uv 0.4.1
✅ Dependencies: 8/8 tools have resolved environments
✅ MCP server: reachable on stdio
⚠️  Auth: No API key configured (team mode requires auth)
✅ Discovery: /.well-known/mcp/server-card.json valid
❌ Client: Claude Desktop config not found at ~/Library/...
   Fix: Run `foldermcp connect claude-desktop`
✅ Schemas: 8/8 tools pass MCP validation
✅ Security: No dangerous configuration patterns detected

2 issues found. Run `foldermcp doctor --fix` to auto-resolve.
```

### 7.9 Serverless & Cloud-Native Deployment

Given the 95% serverless usage in Fortune 500 enterprises and MCP's Docker-first model creating architectural incompatibility, FolderMCP ships native serverless adapters.[^1]

**v1 deployment targets:**

| Target | Adapter | IaC Template | Status |
|---|---|---|---|
| Local | Native process | N/A | ✅ v1 |
| Docker | Dockerfile generator | Docker Compose | ✅ v1 |
| Google Cloud Run | Container + Cloud Run config | Terraform | ✅ v1 |
| AWS Lambda | Lambda package | CDK + SAM | ✅ v1 |
| Azure Functions | Function App | ARM template | 🔶 v1.1 |
| Kubernetes | Helm chart | Helm | 🔶 v2 |
| Cloudflare Workers | Worker adapter | Wrangler config | 🔶 v2 |

Each adapter generates a complete deployment package: infrastructure code, environment variable injection, health check endpoints, and startup configuration. Addresses the "no standard IaC template" blocker identified by practitioners.[^1]

### 7.10 Protocol Compatibility Exports (Non-Runtime)

A2A, ACP, and AGNTCY compatibility artifacts are generated as metadata exports in v1 — not full runtime implementations. This provides value for teams operating in multi-protocol environments without blocking the MCP-first launch.

**v1 exports:**
- `agent-card.json` (A2A format): auto-generated from tool catalog; valid for A2A discovery
- `acp-manifest.json` (ACP format): ACP-style capability declaration
- `oasf-schema.json` (AGNTCY format): OASF-compliant agent schema for AGNTCY directory submission

**v2 (full runtime):**
- A2A server: HTTP + SSE, full task lifecycle, Agent Cards
- ACP server: REST + SSE, async-first, multimodal messages
- AGNTCY: SLIM protocol, directory registration

***

## 8. CLI Reference

```bash
# Initialize a project — scan folder, generate foldermcp.yaml, check dependencies
foldermcp init [path]

# Re-scan for new/changed tools
foldermcp scan

# Review and approve discovered tools (interactive terminal UI)
foldermcp review

# Start the MCP server
foldermcp serve [--mode=dev|team|production] [--port=3000] [--protocols=mcp,a2a]

# Open the local developer studio
foldermcp ui

# Run diagnostics and self-check
foldermcp doctor [--fix]

# Test tool invocations against the running server
foldermcp test [tool-name]

# Deploy to a target environment
foldermcp deploy docker
foldermcp deploy cloudrun
foldermcp deploy lambda
foldermcp deploy azure       # v1.1

# Connect to an MCP client (auto-configures client config files)
foldermcp connect claude-desktop
foldermcp connect cursor
foldermcp connect vscode

# Export compatibility artifacts
foldermcp export a2a
foldermcp export acp
foldermcp export agntcy

# Publish to MCP Registry (Phase 3)
foldermcp register
```

***

## 9. Technical Architecture

### 9.1 Component Map

```
FolderMCP Runtime
├── Watcher          (filesystem events → catalog updates)
├── Introspector     (Python AST, OpenAPI parser, CLI analyzer, README parser)
├── Dependency Mgr   (uv venv builder, npm isolator, per-tool env cache)
├── Catalog          (live tool registry: metadata + approval state + schemas)
├── Auth Layer       (API key, OAuth2/PKCE, mTLS)
├── Router           (profile filtering + semantic tool selection)
├── Protocol Layer
│   ├── MCP Server   (JSON-RPC 2.0, stdio + Streamable HTTP) [v1]
│   ├── A2A Export   (agent-card.json metadata)              [v1]
│   ├── ACP Export   (acp-manifest.json metadata)            [v1]
│   ├── A2A Runtime  (HTTP + SSE, full task lifecycle)        [v2]
│   └── ACP Runtime  (REST + SSE, async-first)               [v2]
├── Sandbox          (subprocess isolation, resource limits, syscall profiles)
├── Audit Logger     (OTLP-compatible, structured JSON, SIEM export)
├── Discovery        (/.well-known/mcp, /.well-known/a2a, AGNTCY dir)
└── Studio UI        (localhost:3001 dashboard, payload inspector, test harness)
```

### 9.2 Language Support

| Language | v1 | v2 |
|---|---|---|
| Python | ✅ Full (AST + uv isolation) | ✅ |
| TypeScript/JavaScript | ✅ Full (ESM/CJS + npm isolation) | ✅ |
| Shell scripts | ✅ Full | ✅ |
| OpenAPI (any language) | ✅ Full | ✅ |
| Go | 🔶 OpenAPI only | ✅ Full |
| Rust | ❌ | 🔶 Basic |
| Java/JVM | ❌ | ✅ |

### 9.3 The Execution Sandbox

FolderMCP wraps all tool execution in isolated sandboxes. This prevents a compromised tool from escaping its process boundary — a direct mitigation for the supply chain attack pattern identified as among the most consequential MCP security risks in 2026.[^20][^24]

- Python: subprocess worker with `seccomp` profile, memory limit, CPU timeout, no network by default (configurable per tool)
- Shell: restricted shell with blocked syscalls, no root filesystem access, no network by default
- Containers: Docker-based tools run with read-only root filesystem and dropped capabilities

***

## 10. Competitive Positioning

### Competitive Landscape

| Product | Strength | Gap vs. FolderMCP |
|---|---|---|
| **Docker MCP Toolkit**[^25][^18] | 300+ verified container-packaged servers, built-in OAuth, one-click client config, SBOM provenance | Catalog-centric; helps consume existing public tools, not expose private code folders |
| **Composio**[^26] | 850+ managed SaaS integrations, enterprise gateway, SOC2, auth handling | Strong for SaaS integrations; weak as folder-native custom code deployment tool |
| **Smithery / Glama**[^6][^27] | Large server discovery directories | Discovery ≠ deployment; no runtime, policy, or approval layer |
| **FastMCP** | Fastest Python MCP framework | No security defaults; no deployment; no multi-protocol |
| **MCP Gateways** (MintMCP, TrueFoundry, etc.)[^28][^29] | Enterprise auth, RBAC, observability | Assume servers already exist; do not solve creation from local code |
| **OpenAPI-to-MCP generators**[^30][^31] | Useful codegen from OpenAPI specs | Partial; no runtime, no security, no deployment |

### Differentiated Position

**FolderMCP is the only product that:**
1. Starts from any existing code folder (not just OpenAPI specs or pre-built containers)
2. Handles dependency isolation automatically
3. Inserts an approval layer before any tool is invocable
4. Ships secure defaults (deny-all, auth required, sandboxed execution)
5. Deploys to serverless architectures natively
6. Provides debugging visibility into agent↔tool payloads
7. Generates compatibility artifacts for multiple protocols without requiring full multi-protocol runtime support on day one

**Do NOT compete on:** Largest catalog, most SaaS integrations, broadest protocol matrix, or workflow automation breadth. Those categories are crowded and not the primary buyer's job-to-be-done.

***

## 11. Business Model

### Open-Core with Buyer-Based Segmentation

The right monetization structure segments by **who cares about the feature** rather than technical location in the codebase:[^32][^33]

**Open-source core (MIT License):**
- Folder scanner and introspector
- Auto-dependency resolution
- Review/approval CLI
- MCP server runtime (stdio + Streamable HTTP)
- Local and Docker deployment
- Basic profiles and tool routing
- `foldermcp doctor`, `foldermcp ui`
- Protocol compatibility exports (A2A/ACP/AGNTCY manifests)

*Target: individual developers, small teams, open-source contributors. Drives bottoms-up adoption and community.*

**FolderMCP Enterprise (paid SaaS / self-hosted):**
- Multi-tenant workspace management
- Fleet observability (monitor 1,000+ FolderMCP deployments)
- Enterprise SSO / SAML / Azure AD integration
- Advanced RBAC with policy bundles
- Centralized audit logging for compliance (SOC2, HIPAA)
- Managed cloud hosting with SLA
- SIEM export connectors (Datadog, Splunk, Azure Monitor)
- Priority support and customer success

*Target: platform engineering teams, enterprise AI infrastructure buyers.*

### Why Open-Core Works Here

Developer tools with active open-source components achieve 45% faster enterprise adoption. Community contributions improve the scanner, add language support, and build ecosystem trust before the enterprise sale. The GitLab model is the right analog: open-source core drives adoption; enterprise governance drives revenue.[^34][^16]

***

## 12. Go-To-Market Strategy

### Phase 1: Developer Wedge

**Target:** AI infra engineers, platform teams, internal tooling builders  
**Message:** "Turn your internal API folder into MCP in 5 minutes"

**Channels:**
- Open-source GitHub repo with excellent README, quickstart, and example repos
- Hacker News launch post
- Reddit communities (r/mcp, r/LocalLLaMA, r/MachineLearning, r/devops)
- Content: "OpenAPI to MCP in 5 minutes," "Secure MCP defaults checklist," "Serverless MCP on Azure Functions"
- Integrations with Claude Desktop, Cursor, VS Code — auto-configure via `foldermcp connect`

**Key metrics:** GitHub stars, weekly active projects, time-to-first-server, developer NPS

### Phase 2: Enterprise Expansion

**Target:** Teams experimenting with MCP but blocked on security, deployment, and compliance  
**Message:** "Bring your own tools. Run them with policy, audit, and cloud deployment."

**Channels:**
- Design partner pilots (3–5 platform teams before building the commercial layer)
- Content targeting CISO/security teams: MCP security posture, audit trail requirements
- Integration with enterprise identity providers (Okta, Azure AD)

### Phase 3: Ecosystem Leverage

**Target:** SaaS vendors wanting AI-ready distribution  
**Message:** "List in every AI client catalog from one deployment"

**Channels:**
- FolderMCP Registry: community tool bundle discovery built on top of OSS adoption
- Partnerships with MCP Registry, AGNTCY directory, Smithery for cross-listing
- `foldermcp register`: one-command publish to all major registries

***

## 13. Success Metrics

### Activation

| Metric | Target |
|---|---|
| Time from install to first working MCP server | < 10 minutes |
| First-run success rate (no setup errors) | > 80% |
| Users who complete scan → review → serve flow | > 60% |
| Users who successfully connect an MCP client | > 50% |

### Core Experience

| Metric | Target | Baseline Today |
|---|---|---|
| Time to first working MCP server | < 10 minutes | ~24 hours[^2] |
| Cold start on Lambda/Cloud Run | < 500ms | 5+ seconds[^1] |
| Token context for 4-server setup | < 8,000 | 60,000+[^11] |
| Auth failures (default insecure incidents) | 0 | Multiple documented[^10][^7] |
| Dependency resolution success rate | > 90% | Not addressed by competitors |

### Quality Gates (Pre-Launch)

- MCP spec conformance test suite: 100% pass
- Security review by third party: zero critical findings
- `foldermcp doctor` accuracy: correctly identifies 95%+ of real setup issues
- 24-hour continuous operation on Lambda, Cloud Run, Docker: verified before v1 release

### Business

| Metric | 3 months | 6 months | 12 months |
|---|---|---|---|
| GitHub stars | 1,000 | 5,000 | 15,000 |
| Weekly active deployments | 500 | 5,000 | 25,000 |
| Enterprise pilots | 3 | 10 | 30 |
| Paid enterprise customers | 0 | 2 | 10 |

***

## 14. Risk Register

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Protocol specs change rapidly | High | High | Modular adapter architecture; version-pinning; automated spec tracking[^17] |
| Docker/Composio ship "bring your own folder" feature | Medium | High | Move faster on approval layer and debugging studio — those are hard to copy quickly |
| Security incident from misused tool exposure | Medium | Very High | Deny-all default; sandbox execution; signed manifests; security audit before v1[^24][^8] |
| Dependency introspection fails on complex projects | Medium | High | Start with well-defined cases; graceful fallback with explicit user-provided requirements |
| Tool description quality too low for agent use | Medium | High | Editable review step; confidence labels; docstring quality prompts |
| MCP picks a clear winner (e.g., serverless becomes natively supported) | Low | Medium | FolderMCP's approval + debug + deploy layer still valuable even if MCP server creation gets easier |
| Supply chain attack via community MCP tools | High | High | Signed manifests, SBOM generation, sandboxed execution per invocation[^20][^7] |
| Monetization: enterprise features too slow to build | Medium | Medium | Design partners validate commercial layer before building; keep open-source core lean |

***

## 15. Roadmap (30-60-90 Day Execution Plan)

### Phase 0: Validation Sprint (Days 1–14)

**Goal:** Confirm the product wedge is real before building.

**Deliverables:**
- 10–15 user interviews with platform engineers, AI infra teams, and product engineers
- Clickable demo or CLI prototype (scan + review flow only)
- Test the three source types: Python functions, OpenAPI specs, shell scripts
- Validate top deployment targets (ask: what environments are you already on?)

**Exit criteria:**
- 70%+ of target users say the value proposition is compelling
- Top 2 source types and top 2 deployment targets confirmed
- At least 3 users want team/shared deployment (validates enterprise tier)

### Phase 1: MVP (Days 15–60)

**Goal:** Working secure MCP exposure layer.

**Scope:**
- [ ] Folder watcher + Python and TypeScript AST introspection
- [ ] OpenAPI spec parser → tool catalog
- [ ] Auto-dependency resolution (uv for Python, npm for JS)
- [ ] Review/approval CLI (terminal UI with risk labels)
- [ ] MCP server: stdio + Streamable HTTP, deny-all default
- [ ] API key auth for remote/team mode
- [ ] TLS auto-generation (self-signed for localhost, Let's Encrypt for production)
- [ ] `foldermcp doctor` with auto-fix for top 10 common issues
- [ ] `foldermcp ui` — local web studio (payload inspector, test harness, token budget)
- [ ] Docker + Cloud Run deployment adapters
- [ ] AWS Lambda adapter
- [ ] MCP conformance test suite: 100% pass
- [ ] Security review: zero critical findings
- [ ] Open-source release on GitHub
- [ ] Quickstart docs: Python folder → MCP in 5 minutes, OpenAPI folder → MCP in 5 minutes
- [ ] Protocol compatibility exports: A2A agent-card, ACP manifest, AGNTCY OASF

**Exit criteria:**
- User reaches first working MCP connection in under 10 minutes
- Dependency resolution works for standard Python/JS imports
- `doctor` catches and explains the top 10 common setup issues
- No critical security findings

### Phase 2: Team Readiness (Days 61–120)

**Goal:** Make it usable for real teams and production environments.

**Scope:**
- [ ] OAuth2/PKCE authentication
- [ ] Azure Functions deployment adapter
- [ ] Kubernetes Helm chart
- [ ] Semantic tool routing (local vector embeddings)
- [ ] Workspace and profile management
- [ ] Structured audit logging with OTLP export
- [ ] Multi-tenant workspace isolation
- [ ] Shell script tool introspection
- [ ] `foldermcp tunnel` — secure tunnel for local→public exposure
- [ ] CI/CD packaging support (GitHub Actions, GitLab CI)

**Exit criteria:**
- 3–5 design partners running in production or staging
- Repeat usage across multiple repos/workspaces
- Enterprise auth story (OAuth2) validated by at least 2 platform teams

### Phase 3: Commercial Layer (Days 121–180)

**Goal:** First paid customers and enterprise narrative.

**Scope:**
- [ ] Hosted enterprise control plane
- [ ] Team management, SSO, SAML
- [ ] Policy management UI
- [ ] SIEM export connectors
- [ ] A2A full runtime (HTTP + SSE, task lifecycle)
- [ ] ACP full runtime (REST + SSE, async)
- [ ] AGNTCY directory registration
- [ ] ANP adapter (if spec matures sufficiently)
- [ ] FolderMCP Registry: search and install community tool bundles
- [ ] `foldermcp register`: one-command publish to MCP Registry + AGNTCY + FolderMCP Registry
- [ ] Go and Rust source introspection

**Exit criteria:**
- First paid enterprise pilots (target: 2–3)
- Repeatable enterprise sales narrative: security posture + deployment + compliance
- Open-source community health: PRs from external contributors, issues response time < 48h

***

## 16. Open Questions

1. **CLI runtime language:** Go or Rust for the FolderMCP daemon? Go is faster to ship; Rust offers better sandbox performance. Decision needed in Week 1.

2. **Offline LLM for descriptions:** Auto-generating tool descriptions from undocumented code requires either an external LLM call or a local model. Default should be offline-first (`all-MiniLM-L6-v2` or similar). What's the acceptable quality bar for auto-generated descriptions without docstrings?

3. **NLIP timing:** Ecma's Natural Language Interaction Protocol is still in progress. Preview adapter in Phase 3 or wait for finalized spec to avoid churn?[^35]

4. **ANP maturity threshold:** The Agent Network Protocol's W3C DID-based identity is promising but still early beta. When does it cross the threshold for a production adapter?[^36]

5. **Design partner selection:** Should the first 3–5 design partners be publicly named (validates enterprise credibility) or kept private (reduces pressure on early-stage product)? Lean toward private for Phase 1, named for Phase 2.

***

## Appendix A: Protocol Stack Reference

```
┌──────────────────────────────────────────────────┐
│              AI Agent / LLM Client               │
│  (Claude, Cursor, VS Code, GPT, custom agent)    │
└───────────┬──────────────────────┬───────────────┘
            │                      │
      Agent↔Tool               Agent↔Agent
            │                      │
┌───────────▼──────┐   ┌───────────▼────────────────┐
│  MCP             │   │  A2A  │  ACP  │  AGNTCY     │
│  (v1 runtime)    │   │  (v1 metadata export only)  │
└───────────┬──────┘   └────────────────────────────┘
            │
┌───────────▼──────────────────────────────────────┐
│           FolderMCP Runtime v1                   │
│  Scan → Review → Approve → Serve → Deploy        │
│  Auth + Sandbox + Routing + Audit + Studio UI    │
└───────────────────────┬──────────────────────────┘
                        │
         ┌──────────────▼──────────────┐
         │       Folder Contents        │
         │  .py  .ts  .sh  openapi.yaml │
         │  + auto-resolved deps        │
         └─────────────────────────────┘
```

## Appendix B: v1.0 → v2.0 Change Summary

| Section | v1.0 | v2.0 Change |
|---|---|---|
| Protocol scope | MCP + A2A + ACP + AGNTCY + ANP all in v1 | MCP runtime v1; A2A/ACP as metadata exports; full runtime in v2 |
| Exposure model | Zero-config auto-publish | Approve before expose (deny-all default) |
| Dependency handling | Not addressed | Auto-isolation via `uv` (Python) / npm (JS) |
| Debugging | Not addressed | `foldermcp ui` local studio |
| Diagnostics | Not addressed | `foldermcp doctor` with `--fix` |
| Competitive framing | Undefined | Positioned vs. Docker MCP Toolkit, Composio on private tool deployment |
| Primary buyer | Generic developers | Platform teams, API owners, AI infra engineers |
| Monetization | Mentioned but not defined | Open-core with buyer-based segmentation[^32] |
| Roadmap | 12-month phases | 30-60-90 day sprint plan with exit criteria |
| Security review | Post-launch | Required gate for v1 release |
| Market framing | "Beat all protocols" | "Own private/custom tool deployment layer" |

---

## References

1. [MCP in enterprise: real-world applications and challenges - Xenoss](https://xenoss.io/blog/mcp-model-context-protocol-enterprise-use-cases-implementation-challenges) - Discover whether MCP is enterprise-ready in 2025. Explore real-world adoption and the challenges ent...

2. [Agentic AI Statistics 2026: 150+ Data Points Collection](https://www.digitalapplied.com/blog/agentic-ai-statistics-2026-definitive-collection-150-data-points) - The definitive collection of 150+ agentic AI statistics for 2026 covering market size, adoption rate...

3. [MCP Server Discovery: Implement .well-known/mcp.json (2026 ...](https://www.ekamoira.com/blog/mcp-server-discovery-implement-well-known-mcp-json-2026-guide) - The answer is server discovery via .well-known/mcp.json endpoints, a standardized mechanism that let...

4. [Anyone else finding MCP server management a pain? What's your ...](https://www.reddit.com/r/mcp/comments/1mbljuu/anyone_else_finding_mcp_server_management_a_pain/) - Dependency conflicts between different servers · Servers randomly breaking after updates · Having to...

5. [From Pain Points to Solutions: How VSCode Solved MCP's Biggest ...](https://workos.com/blog/mcp-night-2-0-demo-recap-vscode-harald-kirschner) - The final major pain point Harald discussed was context bloat—the problem of MCP servers potentially...

6. [MCP Marketplace Guide: Find the Right Server (2026) | Apigene Blog](https://apigene.ai/blog/mcp-marketplace) - Back to Blog

insights

7. [MCP Security: The Protocol Nobody Secured Before Shipping](https://cloudtweaks.com/2026/03/mcp-security-the-protocol-nobody-secured-before-shipping/) - Between January and February 2026, security researchers filed over 30 CVEs targeting MCP servers, cl...

8. [OWASP MCP Top 10](https://owasp.org/www-project-mcp-top-10/) - This OWASP Top 10 for MCP outlines the most critical security concerns arising in the lifecycle of M...

9. [The Complicating Factors of Deploying MCP in the Enterprise](https://goteleport.com/blog/complicating-mcp-enterprise/) - No per-tool authorization: OAuth scopes authorize access to the MCP server, not to individual tools ...

10. [MCP Defaults Will Betray You: The Hidden Dangers of Remote ...](https://cardinalops.com/blog/mcp-defaults-hidden-dangers-of-remote-deployment/) - MCP enables LLM agents to interact with external tools through a server and transport layer. A typic...

11. [Solving the MCP Tool overload problem - Redis](https://redis.io/blog/from-reasoning-to-retrieval-solving-the-mcp-tool-overload-problem/) - After a certain point, giving agents more tools actually made them worse. Tool selection accuracy dr...

12. [The Interoperability Imperative: Orchestrating AI Across the Enterprise](https://www.gcaie.org/post/the-interoperability-imperative-orchestrating-ai-across-the-enterprise) - Most organizations are running dozens of AI pilots. Few are scaling. The root cause isn’t model perf...

13. [AI Tool Switching Is Stealth Friction – Beat It at the Access Layer](https://blog.jetbrains.com/ai/2026/02/ai-tool-switching-is-stealth-friction-beat-it-at-the-access-layer/) - Has your team's sprint velocity actually improved since you approved all those AI coding tools? If n...

14. [The State of Developer Experience 2025: Boosted by AI, But ...](https://pixelit.ca/digital-marketing/the-state-of-developer-experience-2025-boosted-by-ai-but-hindered-by-friction/) - In the latest State of DevEx report, Atlassian surveyed 3,500 developers and managers across six cou...

15. [AI Agents Market Report 2025-2030, by Application, Geo, ...](https://www.marketsandmarkets.com/Market-Reports/ai-agents-market-15761548.html) - [483 Pages] AI Agents Market size was valued at USD 7.84 billion in 2025 and is projected to grow US...

16. [How to Measure Developer Go-To-Market Success in 2026 - Stateshift](https://blog.stateshift.com/how-to-measure-go-to-market-success-for-developer-audiences/) - GitHub's internal metrics reveal that developer tools with active open source components achieve 45%...

17. [Roadmap - Model Context Protocol](https://modelcontextprotocol.io/development/roadmap) - Streamable HTTP gave MCP a production-ready transport, but running it at scale has revealed gaps aro...

18. [Docker MCP Catalog and Toolkit](https://docs.docker.com/ai/mcp-catalog-and-toolkit/) - The Docker MCP Catalog provides 300+ verified servers packaged as container images with versioning, ...

19. [MCP Server: The Dangers of “Plug-and-Play” Code - Acuvity AI](https://acuvity.ai/mcp-server-the-dangers-of-plug-and-play-code/) - You can pin container versions and use security tools to scan images for known vulnerabilities befor...

20. [MCP Supply Chain Security & Risks](https://mcpmanager.ai/blog/mcp-supply-chain-security/) - Here are the main MCP supply chain security risks and the best ways to mitigate each of them. Risk 1...

21. [Specification](https://modelcontextprotocol.io/specification/2025-11-25)

22. [Why MCP Deprecated SSE and Went with Streamable HTTP - fka.dev](https://blog.fka.dev/blog/2025-06-06-why-mcp-deprecated-sse-and-go-with-streamable-http/) - One of the most significant recent changes is the transition from Server-Sent Events (SSE) to Stream...

23. [llms - full.txt - Model Context Protocol](https://modelcontextprotocol.io/llms-full.txt) - The AI application fetches available tools from all connected MCP servers and combines them into a u...

24. [The Reality of MCP Security: A CTO Action Plan](https://obot.ai/blog/mcp-security-cto-action-plan/) - HackerNoon's 2026 review of early MCP incidents places supply chain attacks among the most consequen...

25. [Docker MCP Toolkit: Run MCP Servers Securely](https://www.docker.com/blog/mcp-toolkit-mcp-servers-that-just-work/) - Today, we want to highlight Docker MCP Toolkit, a free feature in Docker Desktop that gives you acce...

26. [The Guide to MCP I never had - Composio](https://composio.dev/content/the-guide-to-mcp-i-never-had) - A specialized reverse proxy between AI agents and tools, providing centralized auth, observability, ...

27. [Smithery vs Glama - nolist.ai](https://nolist.ai/compare/smithery-vs-glama) - Pricing: Both are free for discovery and local use. Smithery is fully open-source; Glama offers prem...

28. [5 portkey MCP gateway alternatives for 2026 | MintMCP Blog](https://www.mintmcp.com/blog/portkey-with-mcp) - 1. MintMCP gateway: Enterprise MCP infrastructure platform​ · 2. TrueFoundry MCP gateway: Ultra-Low ...

29. [5 Best MCP Gateways in 2026 - TrueFoundry](https://www.truefoundry.com/blog/best-mcp-gateways) - Explore the best MCP gateways, including Truefoundry. Compare top scalable platforms for model routi...

30. [OpenAPI to MCP Server Code Generator - GitHub](https://github.com/cnoe-io/openapi-mcp-codegen) - OpenAPI to MCP Server Code Generator. Contribute to cnoe-io/openapi-mcp-codegen development by creat...

31. [harsha-iiiv/openapi-mcp-generator: A tool that converts ... - GitHub](https://github.com/harsha-iiiv/openapi-mcp-generator) - A tool that converts OpenAPI specifications to MCP server - harsha-iiiv/openapi-mcp-generator

32. [A standard pricing model for open core](https://www.opencoreventures.com/blog/a-standard-pricing-model-for-open-core) - The blog post lists six scathing declarations on open core: “'Open core' has NOTHING to do with 'Ope...

33. [7 Open Source Monetization Models - LinkedIn](https://www.linkedin.com/pulse/7-open-source-monetization-models-reo-dev-slauc) - Software-as-a-Service (SaaS) Hosting. In this model, you sell a hosted version of your open-source d...

34. [The Open Source Moat: How GitLab's Developer Community Drove ...](https://www.reo.dev/blog/the-open-source-moat-how-gitlabs-developer-community-drove-11b-in-value) - GitLab's GTM Strategy: A Community-Led Flywheel. From the beginning ... software development journey...

35. [AI agents: A snapshot of the most important protocols for 2025](https://www.linkedin.com/posts/arun-saraswat-44b3741_ai-agents-interoperability-activity-7363424883579514883-H_7l) - AI agents aren’t just hype—they’re already learning how to talk to each other. From integration stan...

36. [Technical Specifications - Agent Network Protocol（ANP）](https://agentnetworkprotocol.com/en/specs/) - The ANP (Agent Network Protocol) technical specifications provide a layered, structured approach to ...

