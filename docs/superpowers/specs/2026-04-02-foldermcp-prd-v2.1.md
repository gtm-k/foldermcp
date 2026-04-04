# PRD v2.1 — FolderMCP: Plug & Play AI Protocol Runtime

**Status:** Approved for Execution
**Version:** 2.1 (Revised from v2.0 — incorporates specialist review)
**Date:** April 2026
**Owner:** Product / Founding Team
**Classification:** Internal — Product Strategy

---

## Executive Summary

Developers are racing to connect their code, scripts, and APIs to AI agents — but every integration still demands bespoke configuration, boilerplate servers, JSON-RPC wiring, and fragile auth setup. The MCP ecosystem has reached critical mass — 97 million monthly SDK downloads and 10,000+ active public servers — yet deploying a production-grade MCP server remains a multi-day effort requiring specialized knowledge.

**FolderMCP** is an open-core, zero-configuration runtime. Point it at any directory containing scripts, OpenAPI specs, or binaries — and it converts the folder into a secure, deployable, MCP-compatible AI tool surface in minutes. No boilerplate. No YAML. No DevOps.

### What Changed in v2.1

v2.0 set the right vision and made critical strategic corrections (MCP-first, approval layer, dependency resolution). v2.1 incorporates feedback from five specialist reviews (market research, product strategy, user research, architecture, hands-on engineering) to sharpen execution feasibility:

1. **Halved MVP scope:** Phase 1 split into 1a (walking skeleton, 30 days) and 1b (completeness, 30 days). Web studio deferred to 1b.
2. **Honest performance claims:** Cold start targets revised from <500ms to <3 seconds. Dependency resolution success rate qualified by scenario.
3. **Platform-tiered sandbox:** Dropped Linux-only `seccomp` claims. Defined isolation tiers that work on Windows, macOS, and Linux.
4. **Named personas with behavioral depth:** Three named user sketches replace generic ICP labels.
5. **Architectural gaps closed:** Added Lifecycle Controller, invocation pipeline, output sanitization, dependency race condition handling, YAML schema versioning.
6. **Resolved open decisions:** Go as CLI language. Template-based descriptions (no LLM required). Per-folder dependency isolation by default.
7. **Resource plan added:** Team size and role allocation now specified.

### One-Line Pitch

> **FolderMCP turns existing code, scripts, and APIs into secure MCP tools in minutes — with review, control, and production-ready deployment built in.**

---

## 1. Problem Statement

### 1.1 The Setup Tax

Despite MCP reaching 97 million monthly SDK downloads and 10,000+ active public servers, the developer experience of deploying a production-grade MCP server remains painful. Real practitioners describe the process as "confusing, inconsistent, and far from intuitive." Documented failure modes include:[^1][^3]

- Cold start delays of up to 5 seconds in serverless environments[^1]
- No standard Infrastructure-as-Code template — engineers figure out all components manually[^1]
- Logging incompatibilities between FastAPI and FastMCP that break standard monitoring[^1]
- Dependency conflicts when running multiple MCP servers simultaneously[^4]
- Full reconfiguration required when switching development environments[^4]
- Average **24 hours** to reach a production-grade MCP deployment (including auth, deployment, monitoring — not just a basic server)[^2]

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

Supporting evidence: the r/mcp subreddit and MCP GitHub discussions contain recurring reports of users whose tools fail on first invocation due to unresolved imports. The official MCP quickstart guides assume pre-installed dependencies, leaving the "real environment" problem to the developer.[^4]

### 1.4 Enterprise Deployment Friction

Serverless architectures (Azure Functions, AWS Lambda, Cloud Run) are the fastest-growing deployment target in enterprise. Many enterprise platform teams are serverless-first, and MCP's Docker-packaged server model creates friction for these teams. As Nayan Paul, Chief Azure Architect at Accenture, wrote: "Unless MCP evolves to support serverless deployment options, I'll likely keep building around it instead of inside it."[^1]

The MCP spec explicitly defers enterprise security to extensions with no dedicated working groups. Gaps include: no mTLS, no per-tool authorization, no audit trail specification, and no gateway behavior specification in the core spec. Five of the OWASP MCP Top 10 risks live directly in these gaps.[^8][^9]

### 1.5 Tool Overload Collapse

When connecting just four common enterprise MCP servers (Redis, GitHub, Jira, Grafana), the combined tool inventory reaches 167 tools consuming ~60,000 tokens before the user speaks a single word. In production environments, 150,000+ token tool contexts are common. Beyond a threshold, adding more tools makes agents measurably worse — tool selection accuracy drops, response times increase, and costs compound.[^11]

### 1.6 The Debugging Black Box

When an agent fails to use a tool correctly, developers currently have no way to inspect the exact JSON-RPC payload sent by the LLM versus what the tool expected. This debugging blindspot kills iteration speed and erodes developer trust in the entire system.

### Problem in One Sentence

> "Teams already have useful tools — exposing them as secure, debuggable, manageable MCP servers is still too much work."

---

## 2. Why This Product Should Exist (Market Validation)

### 2.1 Adoption Signals

MCP is no longer speculative. Verifiable adoption signals:

- **97 million** monthly MCP SDK downloads (MCP project telemetry)[^2]
- **10,000+** active public MCP servers[^2]
- **400%** month-over-month growth in MCP server registrations during peak Q1 2026 (MCP Registry data)[^2]
- **12** Fortune 500 companies have published proprietary MCP servers[^2]
- The AI agents market grows from $7.84B (2025) to $52.62B by 2030 at a 46.3% CAGR (MarketsandMarkets)[^15]
- Developer tools with active open-source components achieve **45% faster enterprise adoption** than proprietary alternatives (GitHub internal metrics)[^16]

*Note: Industry aggregator surveys report 89% enterprise MCP adoption intent and 73% developer preference for MCP. These figures are directionally useful but lack primary research methodology. The verifiable data above is sufficient to establish market readiness.*

### 2.2 Addressable Market (TAM / SAM / SOM)

| Level | Definition | Estimate | Basis |
|---|---|---|---|
| **TAM** | AI agent infrastructure tooling market | ~$8-12B by 2028 | Subset of $52.6B AI agents market; infrastructure/tooling typically 15-20% of total market |
| **SAM** | MCP-specific deployment and management tooling | ~$500M-1B by 2028 | 10,000+ active servers growing 400% MoM; enterprise licensing at $500-5,000/seat |
| **SOM** | Teams deploying private/custom MCP servers needing zero-config tooling | ~$50-100M by 2028 | Bottom-up: est. 5,000-10,000 teams deploying custom tools x $5K-10K avg. annual spend |

These are rough estimates that should be validated during Phase 0 user interviews.

### 2.3 Enterprise Readiness Gap

The official MCP roadmap still lists horizontal scaling, stateless operation, and middleware patterns as active gaps. The spec explicitly defers most enterprise security requirements to extensions not yet shipped. This creates a window for infrastructure products that make MCP deployable in the real world now — not in a future spec version.[^17][^9]

### 2.4 Competitive Gap

Discovery and catalog products are already saturated (Glama: 6,000+ listings, mcp.so: 5,000+, Smithery: 2,000+, Docker MCP Catalog: 300+ verified servers). What does not yet exist is a clean **"bring your own folder, expose it safely, run it anywhere"** deployment path for private and custom tools. That is the unoccupied position FolderMCP should own.[^18][^6]

### 2.5 Timing Thesis

**FolderMCP's window exists because MCP adoption has crossed the tipping point but enterprise deployment infrastructure lags. We believe this window is 6-12 months.**

This window closes if any of the following occur:
- Anthropic ships native folder-to-MCP capability in Claude Desktop
- The MCP spec adds built-in approval flows, auth primitives, or deployment packaging
- Docker MCP Toolkit adds custom code/folder ingestion
- Composio ships a "bring your own code" mode

**Action:** Monitor these signals monthly. If any trigger fires, reassess competitive strategy within 2 weeks.

---

## 3. Target Users

### 3.1 Persona: "Priya" — Staff Platform Engineer (Primary)

**Profile:** Staff Platform Engineer at a Series C fintech, 180 engineers. Priya's team maintains 14 internal microservices and a shared Python utility library. Her VP of Engineering told her the AI team needs "all internal tools accessible to the new coding assistant" by end of quarter.

**Current stack:** Python, AWS (Lambda + ECS), Terraform, Datadog, Okta, GitHub
**Current workaround:** Manually wrapping 3 tools in FastMCP; stopped at auth complexity after 2 days of effort.
**Evaluation behavior:** Tries tools during Thursday afternoon focus blocks. 30-minute evaluation window. If it doesn't work, she goes back to custom FastMCP wrappers.
**Decision authority:** Can adopt for her team; needs VP sign-off for company-wide rollout.
**Anxieties:** "Am I introducing a security vulnerability?" "Will this tool be maintained in 6 months?" "Can I explain this to the security team?"

**Success criteria:** Working PoC she can demo to her VP in a 15-minute meeting.

**Activation metric:** Successful demo of scan-review-serve-deploy to team (target: 2 hours)

### 3.2 Persona: "Marcus" — Full-Stack Product Engineer (Secondary)

**Profile:** Full-stack engineer at an AI startup, 12 engineers. Building an internal AI assistant that needs access to their analytics API and deployment scripts.

**Current stack:** TypeScript, Next.js, Vercel, Cursor, Stripe, PostgreSQL
**Current workaround:** Hardcoded tool definitions in LLM prompts; manually managing API calls. Works "well enough" but doesn't scale.
**Evaluation behavior:** Sees it on Hacker News or X. Tries it immediately if the README is compelling. Will tell teammates at standup if it works.
**Decision authority:** Can adopt independently.
**Anxieties:** "Another tool to learn." "Will this add complexity to my stack?" "Can I get back to building features?"

**Success criteria:** AI assistant can query the analytics DB within a single coding session.

**Activation metric:** First successful tool call from AI client (target: 10 minutes)

### 3.3 Persona: "Dana" — DevOps/SRE (Operational)

**Profile:** SRE at the same fintech as Priya. Priya hands her a FolderMCP deployment to run in staging.

**Current stack:** Kubernetes, Helm, ArgoCD, Grafana, PagerDuty
**Evaluation behavior:** Checks resource requirements, log formats, health check endpoints, restart behavior, upgrade paths.
**Decision authority:** Can block production deployment on operational grounds.
**Anxieties:** "Will this page me at 2 AM?" "What happens when it crashes?" "What are the resource requirements?"

**Success criteria:** FolderMCP runs reliably for 30 days with no manual intervention.

**Activation metric:** 24-hour run in staging with clean logs (target: 1 day)

### 3.4 Persona: "Raj" — Security/Compliance Reviewer (Gate Approver)

**Profile:** Security engineer at an enterprise using FolderMCP. Must approve any tool that exposes internal APIs to AI agents. Has veto power.

**Evaluation behavior:** Reviews audit logs, auth model, sandbox boundaries, data exposure surface. Needs artifacts (audit reports, security documentation, compliance checklists).
**Decision authority:** Can block or approve deployment.

**Success criteria:** Can produce a security assessment document for the CISO within 2 hours.

### 3.5 Tertiary: SaaS Vendors with Existing APIs

SaaS companies wanting to expose their product as an AI-accessible tool by wrapping existing OpenAPI specs into an MCP-compatible interface. This persona maps functionally to Priya (large SaaS) or Marcus (small SaaS) depending on company size. Distinct as a JTBD variation, not a distinct persona.

Becomes a dedicated persona when Phase 3 (Registry/ecosystem) activates.

---

## 4. Jobs To Be Done

### Core Functional JTBDs

1. **Expose existing tools to AI:** When a team has useful code or APIs already working, help them expose those capabilities to AI clients quickly and safely, without building custom integration infrastructure.
2. **Secure by default:** When deploying to production, provide safer auth, policy, audit, and cloud deployment defaults than the ecosystem standard.
3. **Manage tool overload:** When a tool catalog grows large, help agents discover only the relevant tools instead of receiving every tool in context.
4. **Debug agent-tool interactions:** When an agent fails to use a tool correctly, help the developer understand what happened — what was sent, what was expected, where it broke — in under 5 minutes.
5. **Keep tools in sync with code:** When code is updated, MCP tools should reflect changes automatically without manual re-deployment or re-configuration.

### Emotional & Social JTBDs

6. **Feel confident about security:** When exposing internal tools to AI, help the developer feel confident they are not introducing a vulnerability that will come back to haunt them.
7. **Look competent to the team:** When demoing AI tool infrastructure to stakeholders, provide a tool that works reliably on the first demo.
8. **Advocate internally:** Help the developer advocate for this tool by providing artifacts (audit logs, security reports) that managers and CISOs care about.

### User Stories

**Happy-path:**
- As Priya, I want to point FolderMCP at an internal repo and get a secure MCP endpoint I can deploy behind Okta auth, without writing any MCP server code.
- As Marcus, I want my OpenAPI specs turned into MCP tools I can invoke from Cursor in a single coding session.
- As a security-conscious team, I want deny-by-default behavior, audit logs, and per-tool policies before any tool is callable.
- As Dana, I want to monitor FolderMCP's resource usage, log output, and health from Grafana/Datadog.
- As Raj, I want to review which tools are exposed, their risk labels, and the audit trail before approving for production.

**Failure-path:**
- As a developer, when `foldermcp init` fails because my Python version is unsupported, I want a clear error telling me exactly what to do.
- As a developer, when auto-dependency resolution guesses wrong (e.g., `import cv2` doesn't resolve to `opencv-python`), I want to override the dependency without losing auto-resolution for other imports.
- As a developer, when a tool invocation fails at runtime, I want to see the exact error (stack trace, stderr, exit code), not just "tool execution failed."
- As Dana, when a tool is consuming too much memory, I want it auto-killed and the incident logged.
- As a team lead, when a new developer joins, I want a documented onboarding path that takes under 15 minutes.

**Migration:**
- As a team already running FastMCP servers, I want to import existing tool definitions into FolderMCP without rewriting them.
- As a developer with existing `claude_desktop_config.json` entries, I want FolderMCP to coexist with my other MCP servers, not replace them.

**Maintenance:**
- As an operator, I want to rotate API keys and TLS certificates without downtime.
- As a platform engineer, I want to upgrade FolderMCP versions without re-approving all my tools.
- As an audit reviewer, I want to export a report of all tools, their approval states, who approved them, and when.

---

## 5. Product Principles

1. **MCP-first, not protocol-maximalist** — Win the most urgent market segment before expanding.
2. **Discover automatically, expose intentionally** — Auto-discovery is a feature; auto-exposure is a risk.
3. **Secure by default** — The absence of auth should require explicit opt-in, never the default.
4. **Fast path to first value** — From `foldermcp init` to a working AI tool call in under 10 minutes (for Marcus). Under 2 hours to a demoable PoC (for Priya).
5. **Production credibility over demo magic** — Every feature must work behind a corporate proxy, on a CI server, and on a developer laptop — not just in a live demo.
6. **Make debugging visible** — Developers should see exactly what flows between the agent and their tools.
7. **Local-first, cloud-optional** — Your code never leaves your machine unless you explicitly deploy. No external API calls required. No telemetry unless opted in.
8. **Extensible by design** — Make it easy for the community to add language support, deployment targets, and auth providers.
9. **Protocol expansion only after proven wedge** — A2A/ACP become real runtime targets once MCP adoption is proven.

---

## 6. Anti-Goals (What We Will Not Build)

- **Not a tool marketplace.** FolderMCP will not become a catalog or registry of pre-built integrations. That is Composio and Smithery's space.
- **Not a workflow orchestrator.** FolderMCP exposes tools; it does not chain them into multi-step workflows.
- **Not a SaaS integration platform.** We do not build connectors to Slack, Jira, or Salesforce. We help you expose YOUR code.
- **Not a replacement for FastMCP.** FastMCP is a framework for writing MCP servers. FolderMCP wraps existing code without requiring you to write MCP server code. They are complementary.
- **Not a full multi-protocol runtime in v1.** MCP is the runtime. A2A/ACP are metadata exports until demand proves otherwise.

---

## 7. What Changed from v1.0 → v2.0 → v2.1

| Dimension | v1.0 | v2.0 | v2.1 |
|---|---|---|---|
| Protocol scope | All 5 protocols | MCP-first; A2A/ACP metadata | Same |
| Exposure model | Zero-config publish | Approve before expose | Tiered review by mode (dev/team/prod) |
| Security posture | Deny-all default | Deny-all + approval flow | Platform-tiered sandbox; output sanitization added |
| Dependency handling | Not addressed | Auto-isolation per-tool | Per-folder default; per-tool opt-in; curated mapping |
| Developer debugging | Not addressed | `foldermcp ui` in MVP | UI deferred to Phase 1b; CLI verbose + test in 1a |
| Architecture | Not specified | Component map | Added Lifecycle Controller, invocation pipeline, YAML versioning |
| Personas | Generic developers | Platform teams, API owners | Named personas with behavioral depth + SRE + Security |
| Performance claims | Not specified | <500ms cold start | <3 seconds (honest target) |
| Resource plan | Not specified | Not specified | Team size and roles defined |
| CLI language | Not decided | Open question | **Go** (decided) |
| Description generation | Not specified | LLM-assisted (offline model) | Template-based (no LLM required for v1) |
| MVP scope | 12-month roadmap | 30-60-90 day plan | Phase 1a (30d) + 1b (30d) split |

---

## 8. Core Feature Specifications

### 8.1 Intelligent Folder Scanner

FolderMCP watches the target directory using filesystem events and maintains a live catalog of discoverable tools.

**Supported inputs:**

| Source Type | What's Detected | v1 | v2 |
|---|---|---|---|
| Python files | Top-level functions, annotated class methods, signatures, docstrings | ✅ | ✅ |
| TypeScript/JavaScript | Exported functions (ESM/CJS) | ✅ (Phase 1b) | ✅ |
| OpenAPI specs | All operations, parameters, schemas | ✅ | ✅ |
| Shell scripts | Command-line tool wrapper | ✅ | ✅ |
| `agent-card.json` | A2A-compatible agent metadata | 🔶 | Compatibility export |
| `*.csv`, `*.parquet`, `*.json` | Data resources | 🔶 | Phase 2 |
| Dockerfile | Containerized service | 🔶 | Phase 2 |

**Scan scope configuration:** Users configure which directories and file patterns to scan via `foldermcp.yaml`, preventing test fixtures, build artifacts, and migration scripts from being discovered as tools.

```yaml
scan:
  include:
    - "src/**/*.py"
    - "api/**/*.yaml"
  exclude:
    - "tests/**"
    - "migrations/**"
    - "__pycache__/**"
```

**Behavior:**
- Deep scan on startup, live-reload on file changes (detected within 2 seconds)
- Template-based description generation from function names, parameter names, type hints, and docstrings (no external API calls, no LLM required)
- Auto-generates tool names, input schemas, and invocation wrappers
- Unsupported files are silently skipped with an optional verbose log
- `@foldermcp.tool` decorator available as escape hatch for functions where AST inference fails (e.g., `*args/**kwargs`, complex decorators, dynamic behavior)

**Description generation approach (v1):**
- Functions WITH type hints + docstrings: use docstring as description, generate JSON Schema from type hints (~90% accuracy)
- Functions WITH docstrings only: parse docstring for parameter descriptions (~65% accuracy)
- Functions with neither: template-based — `"Calls {function_name} with parameters: {param_list}"` (~20% accuracy, clearly marked as auto-generated)
- Users can override any description in `foldermcp.yaml`

**Acceptance criteria:**
- A folder with 10 Python files is fully cataloged in under 10 seconds (excluding LLM-based enrichment)
- Generated tool descriptions pass MCP schema validation without manual editing for 85%+ of functions with type hints and docstrings
- Users can override any auto-generated name, description, or schema via `foldermcp.yaml`
- Scan scope configuration correctly excludes test/build directories

### 8.2 Auto-Dependency Resolution

A Python tool that imports `pandas` fails silently in the runtime without environment isolation. FolderMCP eliminates this blocker.

**Behavior:**
- On scan, FolderMCP inspects `import` statements in Python files and `require`/`import` in TypeScript
- If a `requirements.txt`, `pyproject.toml`, or `package.json` exists, it is used directly (preferred path)
- If not, dependencies are inferred from import statements using a curated mapping of the **top 500 import-name-to-PyPI-package-name pairs** (community-maintainable JSON file)
- Default isolation is **per-folder** (one virtual environment per scanned directory). Per-tool isolation available as opt-in for known conflict cases.
- Automatically builds isolated virtual environments using `uv` (Python) and a pinned `node_modules` (JS/TS)
- Dependency isolation runs in the background — does not block `foldermcp serve`
- Tools with unresolved dependencies enter `RESOLVING` state — visible in catalog but return "dependency installation in progress, retry in N seconds" if invoked
- Environment is cached and only rebuilt when dependencies change
- Generates `foldermcp.lock` to pin exact resolved versions for reproducibility

**When inference fails:**
- Loud, actionable error: `"Cannot find PyPI package for 'import cv2'. Add 'opencv-python' to requirements.txt or foldermcp.yaml dependencies."`
- Never fail silently. Always tell the user what to do.
- Fallback: user provides explicit dependencies in `foldermcp.yaml`:

```yaml
dependencies:
  python:
    - opencv-python>=4.8
    - Pillow>=10.0
```

**Realistic success rates:**

| Scenario | Expected Success Rate |
|---|---|
| Projects WITH requirements.txt/pyproject.toml | 95%+ |
| Projects WITHOUT, using top-200 PyPI packages | 75-80% |
| Projects WITHOUT, using niche/namespace packages | 40-60% |
| Blended real-world | 70-80% |

**Acceptance criteria:**
- A Python script importing `pandas`, `requests`, and `boto3` with no `requirements.txt` runs successfully once dependency resolution completes
- Tools with unresolved dependencies show `RESOLVING` state and return actionable error if invoked before ready
- Conflicting dependencies across tools in different folders are isolated
- `foldermcp.lock` is generated and used for subsequent runs
- Failed resolution produces a clear, actionable error message

### 8.3 Review & Approval Layer

The security reality of the MCP ecosystem — OWASP Top 10, 30+ CVEs, 41% of registry servers without auth — makes automatic tool publishing unacceptably risky. FolderMCP separates discovery from exposure.[^6][^7][^8]

**Tiered review flow (scaled to deployment context):**

| Mode | Default Behavior | Review Experience |
|---|---|---|
| `dev` | Bulk approval with informed consent | "47 tools found, 12 have side-effects. Approve all for local dev? [y/N]" |
| `team` | Risk-based batching | "Approve 35 read-only tools? Review 12 side-effect tools individually?" |
| `production` | Full individual review with audit trail | Each tool reviewed individually; approval recorded with timestamp and identity |

**Behavior:**
- After a scan, all discovered tools are in `PENDING` state — visible in the catalog but NOT invocable by connected clients
- `foldermcp review` presents either a CLI batch flow (default) or an interactive terminal UI (opt-in via `--tui`)
- Each tool shows: name, source file, auto-generated description, inferred input schema, risk label (Read-only / Side-effects / Destructive / Network)
- Developer marks each tool as `ENABLED`, `DISABLED`, or `REQUIRES_CONFIRMATION`
- `REQUIRES_CONFIRMATION` tools prompt the connected AI client for user approval before each invocation
- In `--mode=production`, new tools discovered after initial approval default to `DISABLED` until re-reviewed
- `tools/list` MCP endpoint only returns `ENABLED` and `REQUIRES_CONFIRMATION` tools — `PENDING` and `DISABLED` tools are invisible to connected clients (prevents token waste)
- Batch-mode CLI flags available as reliable fallback: `foldermcp review --approve query_db --approve list_files --disable deploy_to_prod`

**`foldermcp.yaml` approval state example:**
```yaml
version: 1
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
- No tool is invocable until explicitly approved
- Dev mode bulk approval completes in under 30 seconds for any folder size
- Team mode risk-based batching completes in under 5 minutes for 50 tools
- Production mode records approver identity and timestamp
- Risk labels are accurate for 90%+ of standard Python/OpenAPI tools
- Approval state persists across restarts
- Batch-mode CLI flags work in CI/CD environments (no TTY required)

### 8.4 Secure Multi-Mode Runtime

FolderMCP inverts the ecosystem's insecure defaults. Security should be the zero-config path, not the opt-in.

**Runtime modes:**

| Mode | Auth | TLS | Tools Default | Audit Log | Use Case |
|---|---|---|---|---|---|
| `dev` | None required | Self-signed | Bulk-approved tools only | Local file | Local iteration |
| `team` | API key required | TLS enforced | Deny-all for new tools | Structured JSON | Shared team server |
| `production` | OAuth2/PKCE or mTLS | TLS enforced | Deny-all | OTLP export | Enterprise deployment |

**Security capabilities (v1):**
- **Per-tool authorization:** Connecting to the server does NOT grant access to all tools. Each tool has its own policy state[^9]
- **Execution sandboxing (platform-tiered):**

| Platform | Isolation Level | Capabilities |
|---|---|---|
| All platforms (Tier 1) | Subprocess with resource limits | Memory limit, CPU timeout, process kill on timeout |
| Linux (Tier 2) | Tier 1 + `seccomp` profiles | Restricted syscalls, network namespace isolation |
| All platforms (Tier 3) | Docker-based isolation | Read-only root filesystem, dropped capabilities |

- **Secret injection:** Secrets read from `.env` file or OS keychain. Scoped per-tool via namespace prefixes (`FOLDERMCP_TOOL_<name>_<KEY>`). Each subprocess receives only its own tool's secrets. Never written to disk or logged.
- **Audit log fields:** Tool name, invocation parameters (with secret redaction), calling identity, authorization decision, timestamp, session context, result status
- **Tamper detection:** FolderMCP generates an HMAC signature of the tool catalog at startup; drift alerts on unexpected changes. *Note: HMAC detects accidental drift, not adversarial attacks. Asymmetric signing (Sigstore model) planned for v2/enterprise.*

**Output sanitization (v1):**
- Maximum output size per tool (configurable, default 100KB)
- Regex-based scan for known secret patterns (AWS keys, API tokens) in tool output — redacts and warns
- Structured envelope wrapping for safe agent consumption

### 8.5 MCP Server Runtime

FolderMCP implements the MCP spec version 2025-11-25, wrapping the `mcp-go` SDK with custom auth and approval middleware.[^21]

- **Transports:** stdio (local), Streamable HTTP (remote — Phase 1b) — SSE deprecated per spec direction[^22]
- **Primitives:** Tools, Resources, Prompts (Phase 2), Tasks/async (Phase 2)
- **Discovery:** Auto-publishes `/.well-known/mcp/server-card.json` for auto-discovery by Claude Desktop, ChatGPT, VS Code, Cursor, and other MCP clients[^3]
- **Client compatibility:** Claude Desktop, Cursor, VS Code Copilot, Windsurf, Continue.dev, Goose, and all MCP-spec-compliant clients[^23]

**Invocation pipeline (explicit call chain):**

```
HTTP/stdio Request
  → Transport Decoder (JSON-RPC parse)
  → Server Auth Middleware (identity verification)
  → Request Router (map method to handler)
  → tools/call handler
    → Catalog Lookup (does tool exist?)
    → Policy Enforcer (is caller authorized? is tool ENABLED?)
    → Confirmation Gate (if REQUIRES_CONFIRMATION, request user approval)
    → Sandbox Allocator (get/create worker for tool's environment)
    → Tool Executor (invoke in sandbox, enforce timeout)
    → Result Sanitizer (enforce output size, redact secrets, wrap response)
    → Audit Logger (record invocation)
  → Response Encoder
```

**Error taxonomy:**

| Error Code | Meaning | Agent Action |
|---|---|---|
| `TOOL_EXECUTION_ERROR` | Tool raised an exception | Inform user, show error detail |
| `TOOL_TIMEOUT` | Execution exceeded time limit | Retry with simpler input or inform user |
| `TOOL_DEPENDENCY_ERROR` | Environment not ready (RESOLVING state) | Retry after delay |
| `TOOL_AUTHORIZATION_DENIED` | Policy check failed | Inform user, suggest approval |
| `TOOL_CONFIRMATION_REQUIRED` | User approval needed | Request approval via client UI |

**Acceptance criteria:**
- All MCP conformance tests pass before v1 release
- Local startup (stdio) in under 5 seconds for typical projects
- A connected client can list, inspect, and invoke all approved tools
- Unapproved tools are invisible to connected clients

### 8.6 Semantic Tool Routing

Built-in tool routing prevents the token explosion that collapses agent performance at scale.[^11]

**v1 approach (rule-based):** Profiles and namespaces for grouped tool sets, with a configurable maximum tool count per session. Fast to ship, easy to debug. Default for all deployment modes.

**v2 approach (semantic):** Local vector index of tool descriptions using `all-MiniLM-L6-v2`; tools retrieved by semantic similarity to the incoming query. Opt-in enhancement for local/VM deployments. Disabled by default in serverless (memory-constrained) environments.

**Configuration:**
```yaml
tool_routing:
  max_tools_per_context: 20
  strategy: profile         # or: semantic (v2), namespace, all
  profiles:
    read_only: [query_db, list_files, search_docs]
    billing: [charge_card, refund, view_invoice]
    dev_tools: [run_tests, lint, format_code]
```

**Acceptance criteria:**
- A folder with 100 tools exposes a maximum of 20 to any single agent context by default
- Profile switching applies without restarting the server
- Semantic routing accuracy validated against a test set of agent queries before v2 release

### 8.7 Developer Observability

**Phase 1a — CLI-based debugging:**
- `foldermcp serve --verbose`: Logs all JSON-RPC payloads (request + response), latency, and status to stdout/file
- `foldermcp test <tool-name> --params '{"x": 1}'`: Invoke any tool directly from CLI, see full input/output without needing a running server or connected client
- `foldermcp catalog`: Print tool catalog as a formatted table (name, state, risk, description quality score)
- `foldermcp status`: Show server state — running/stopped, port, enabled tool count, dependency errors
- `foldermcp logs`: Tail the audit/invocation log

**Phase 1b — Web Studio (`foldermcp ui`):**
- Live invocation log: every tool call — exact JSON payload, response, latency, status
- Token budget dashboard: context consumption per session with warnings
- Manual test harness: invoke any tool with custom parameters via web form
- Tool catalog browser: schemas, approval states, description quality scores
- Dependency status panel: venv status, unresolved dependencies

**Architecture:** Lightweight embedded web server (Go `embed` package for static assets), runs as sidecar to `foldermcp serve`, accessible at `http://localhost:3001`. Read-only by default.

**Acceptance criteria:**
- CLI verbose logging shows payloads within 500ms of a tool call
- `foldermcp test` works without a running server (direct catalog execution)
- Studio UI starts in under 3 seconds (Phase 1b)
- Token count estimates within 10% of actual tokenizer counts (Phase 1b)

### 8.8 `foldermcp doctor` Diagnostics

A self-diagnostic command that reduces support burden and increases first-run success rates.[^5][^1]

**What `doctor` checks:**
- Runtime dependencies present (Go, Python/Node versions, uv, Docker)
- All approved tools have working dependency environments
- MCP server is reachable from localhost
- Auth configuration is valid (for non-dev modes)
- At least one MCP client is configured to connect (detects Claude Desktop config, VS Code settings)
- No port conflicts
- TLS certificate validity (for team/production modes)
- Tool descriptions pass MCP schema validation
- Known dangerous configuration patterns (e.g., production mode with no auth)
- `foldermcp.yaml` schema version compatibility

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

**Graceful degradation:** When a component is unavailable:
- Watcher fails (e.g., network filesystem): falls back to periodic polling, warns via doctor
- `uv` not on PATH: disables Python auto-dependency resolution, warns user to provide `requirements.txt`
- Docker not installed: Docker-based tools marked `UNAVAILABLE` with clear message
- Embedding model fails to load: semantic routing disabled, falls back to profile-based

### 8.9 Serverless & Cloud-Native Deployment

**v1 deployment targets:**

| Target | Adapter | IaC Template | Phase |
|---|---|---|---|
| Local | Native process | N/A | ✅ 1a |
| Docker | Dockerfile generator | Docker Compose | ✅ 1a |
| Google Cloud Run | Container + Cloud Run config | Terraform | ✅ 1b |
| AWS Lambda | Lambda package | SAM template | 🔶 1.1 |
| Azure Functions | Function App | ARM template | 🔶 1.1 |
| Kubernetes | Helm chart | Helm | 🔶 v2 |

**Lambda/serverless handler mode:** Serverless deployments use a distinct `FolderMCPHandler` mode:
- No filesystem watcher (catalog pre-built at deploy time)
- No background dependency resolution (deps bundled in deployment artifact)
- No Studio UI sidecar
- Handler receives MCP request → catalog lookup → execute → return
- Secrets fetched from platform-native secrets manager (AWS Secrets Manager, GCP Secret Manager) on cold start, cached for execution lifetime

Each adapter generates a deployment package: infrastructure code, environment variable injection, health check endpoints, and startup configuration. Adapters are "opinionated starter" quality — sensible defaults with documented known limitations.

**Acceptance criteria:**
- Docker adapter produces a working container from any scanned folder
- Cloud Run adapter deploys successfully with provided GCP credentials
- Cold start (FolderMCP runtime, excluding first tool invocation): < 3 seconds
- IaC templates include `--dry-run` preview: `foldermcp deploy docker --dry-run` prints generated files without writing

### 8.10 Protocol Compatibility Exports (Non-Runtime)

**v1 exports (A2A only):**
- `agent-card.json` (A2A format): auto-generated from tool catalog; valid for A2A discovery

*ACP and AGNTCY exports deferred to Phase 2 — those specs are pre-1.0 and evolving rapidly. Shipping one stable export reduces maintenance surface by 66%.*

**v2 (full runtime):**
- A2A server: HTTP + SSE, full task lifecycle, Agent Cards
- ACP server: REST + SSE, async-first, multimodal messages
- ACP/AGNTCY metadata exports

---

## 9. CLI Reference

```bash
# Initialize a project — scan folder, generate foldermcp.yaml, check dependencies
# Interactive: walks through scan → review → optional serve in one flow
foldermcp init [path]

# Review and approve discovered tools
foldermcp review [--tui] [--approve <tool>...] [--disable <tool>...]

# Start the MCP server
foldermcp serve [--mode=dev|team|production] [--port=3000] [--verbose]

# Show server status (running, port, tool count, errors)
foldermcp status

# Tail audit/invocation logs
foldermcp logs [--follow]

# Test a tool invocation directly (no running server required)
foldermcp test <tool-name> [--params '{"key": "value"}']

# Print the tool catalog
foldermcp catalog

# Run diagnostics and self-check
foldermcp doctor [--fix]

# Open the local developer studio (Phase 1b)
foldermcp ui

# Deploy to a target environment
foldermcp deploy docker [--dry-run]
foldermcp deploy cloudrun [--dry-run]
foldermcp deploy lambda [--dry-run]     # v1.1

# Connect to an MCP client (auto-configures client config files)
foldermcp connect claude-desktop
foldermcp connect cursor               # v1.1
foldermcp connect vscode               # v1.1

# Export compatibility artifacts
foldermcp export a2a

# Publish to MCP Registry (Phase 3)
foldermcp register
```

**Design notes:**
- `foldermcp init` is an interactive flow that collapses scan → review → serve into one session (like `npx create-next-app`)
- `foldermcp connect` ships for Claude Desktop in Phase 1a; other clients in 1.1 (Claude Desktop has the simplest, most stable config format)
- All commands support `--json` output for scripting/CI integration
- `review` supports both batch-mode flags (CI-friendly) and optional TUI (developer-friendly)

---

## 10. Technical Architecture

### 10.1 Component Map

```
FolderMCP Runtime
├── Lifecycle Controller (state machine: scan → catalog → deps → review → serve)
├── Watcher              (filesystem events → triggers rescan)
├── Introspector         (plugin interface: Python, OpenAPI, Shell, TS parsers)
├── Dependency Manager   (uv venv builder, npm isolator, per-folder env cache, lockfile)
├── Tool Registry        (metadata + schemas)
├── Policy Store         (approval states, risk labels, per-tool auth policies)
├── Dependency State     (resolution status per tool: RESOLVED / RESOLVING / FAILED)
├── Auth Layer           (API key, OAuth2/PKCE [v2], mTLS [v2])
├── Policy Enforcer      (per-tool authorization at invocation time)
├── Router               (profile filtering + semantic tool selection [v2])
├── Protocol Layer
│   ├── MCP Server       (JSON-RPC 2.0, stdio + Streamable HTTP) [v1]
│   └── A2A Export       (agent-card.json metadata)              [v1]
├── Sandbox              (platform-tiered: subprocess + resource limits → seccomp → Docker)
├── Result Sanitizer     (output size limits, secret redaction, structured envelope)
├── Audit Logger         (structured JSON, OTLP-compatible [v2], SIEM export [enterprise])
├── Discovery            (/.well-known/mcp/server-card.json — owned by Protocol Layer)
└── Studio UI            (localhost:3001 dashboard — Phase 1b)
```

**Introspector Plugin Interface:**
```go
type IntrospectorPlugin interface {
    CanHandle(filePath string) bool
    ExtractTools(filePath string) ([]ToolMetadata, error)
    InferDependencies(filePath string) ([]Dependency, error)
}
```

Each language parser implements this interface. Community contributors can add language support without modifying the core.

### 10.2 State Store Architecture

| Store | Backing | Contents |
|---|---|---|
| `foldermcp.yaml` (user-authored) | YAML file in project root | Configuration: scan scope, tool overrides, routing profiles, mode, secrets references |
| `foldermcp.lock` (generated) | Lockfile in project root | Pinned dependency versions for reproducibility |
| `.foldermcp/state.db` (internal) | SQLite | Runtime state: scan results, approval states, dependency resolution status, cached schemas, audit log |

`foldermcp.yaml` has a required `version: 1` field for schema migration support.

### 10.3 Language Support

| Language | Phase 1a | Phase 1b | v2 |
|---|---|---|---|
| Python | ✅ Full (AST + uv isolation) | ✅ | ✅ |
| TypeScript/JavaScript | — | ✅ Full (ESM/CJS + npm) | ✅ |
| OpenAPI (any language) | ✅ Full | ✅ | ✅ |
| Shell scripts | ✅ Full | ✅ | ✅ |
| Go | — | — | ✅ Full |
| Rust | — | — | 🔶 Basic |
| Java/JVM | — | — | ✅ |

### 10.4 CLI Runtime

**Decision: Go**

| Factor | Go | Rust |
|---|---|---|
| Time to MVP | Achievable in 60 days | Adds 30-50% dev time |
| Subprocess management | `os/exec` is mature | Comparable but more boilerplate |
| Embedded web server | Trivial with `net/http` | More boilerplate |
| Single-binary distribution | `go build` for any OS/arch | Equivalent |
| Contributor pool | Larger in infra/DevTool space | Smaller, higher barrier |
| Ecosystem | `cobra` (CLI), `bubbletea` (TUI), `mcp-go` (protocol) | Less mature MCP ecosystem |

If FolderMCP succeeds and sandbox performance becomes a critical differentiator, security-critical subsystems can be rewritten in Rust as isolated libraries.

---

## 11. Competitive Positioning

### 11.1 Competitive Landscape

| Product | Strength | Gap vs. FolderMCP |
|---|---|---|
| **Docker MCP Toolkit**[^25][^18] | 300+ verified containers, built-in OAuth, one-click config, SBOM | Catalog-centric; doesn't expose private code folders |
| **Composio**[^26] | 850+ managed SaaS integrations, SOC2, enterprise gateway | Strong for SaaS; weak as folder-native custom code tool |
| **Smithery / Glama**[^6][^27] | Large server discovery directories | Discovery ≠ deployment; no runtime or policy |
| **FastMCP** | Fastest Python MCP framework | No security defaults; no deployment; no approval layer |
| **MCP Gateways** (MintMCP, TrueFoundry)[^28][^29] | Enterprise auth, RBAC, observability | Assume servers already exist; don't solve creation |
| **OpenAPI-to-MCP generators**[^30][^31] | Useful codegen | Partial; no runtime, security, or deployment |

### 11.2 Platform Risk

| Platform Actor | Likely Move | Trigger Signal | FolderMCP Response |
|---|---|---|---|
| **Anthropic** | Native folder-to-MCP in Claude Desktop | Anthropic blog post, Claude Desktop changelog | Differentiate on multi-client support, deployment breadth, and enterprise features (auth, audit, sandbox) |
| **Docker** | Add folder/code ingestion to MCP Toolkit | Docker blog, MCP Toolkit changelog | Differentiate on approval layer, debugging studio, and serverless deployment |
| **Composio** | "Bring your own code" mode | Composio changelog, marketing | Differentiate on open-source, local-first, zero-vendor-lock |
| **MCP Spec** | Native auth/deployment primitives | MCP roadmap updates, spec PRs | FolderMCP's value is the integrated workflow (scan→review→deploy→debug), not any single primitive |

### 11.3 Defensibility Analysis

**Temporary advantages (land-grab):** Being first to scan folders, generate approval flows, produce deployment artifacts. These are technically reproducible within a quarter.

**Durable advantages (build over time):**
- Community ecosystem and contributor base (language plugins, deployment adapters)
- Enterprise deployment breadth and battle-tested IaC templates
- Security posture and compliance certifications (SOC2, audit trail depth)
- Developer experience quality (debugging tools, doctor diagnostics, DX polish)
- Trust and brand as "the secure way to expose tools to AI"

### 11.4 Differentiated Position

**FolderMCP is the only product that takes you from existing code to a running, secured, deployed MCP endpoint in a single workflow: scan → review → approve → serve → deploy.**

**Do NOT compete on:** Largest catalog, most SaaS integrations, broadest protocol matrix, or workflow automation breadth.

---

## 12. Business Model

### 12.1 Open-Core with Buyer-Based Segmentation

**Open-source core (Apache License 2.0):**
- Folder scanner and introspector (all languages)
- Auto-dependency resolution
- Review/approval CLI
- MCP server runtime (stdio + Streamable HTTP)
- Local and Docker deployment
- Basic profiles and tool routing
- `foldermcp doctor`, `foldermcp test`, `foldermcp ui`
- A2A compatibility export
- `foldermcp connect` (Claude Desktop)

**FolderMCP Team ($29-49/seat/month — subject to validation):**
- Cloud deployment adapters (Cloud Run, Lambda, Azure Functions)
- API key auth + TLS management
- Structured audit logging with 90-day retention
- Team workspace management
- Priority issue response

**FolderMCP Enterprise (custom pricing):**
- Multi-tenant workspace management
- Fleet observability (1,000+ deployments)
- Enterprise SSO / SAML / Azure AD
- Advanced RBAC with policy bundles
- Centralized audit logging for compliance (SOC2, HIPAA)
- Managed cloud hosting with SLA
- SIEM export connectors (Datadog, Splunk, Azure Monitor)
- OAuth2/PKCE and mTLS auth
- Priority support and customer success

*Pricing is directional and subject to Phase 0 design partner validation.*

### 12.2 Expansion Triggers

What makes a solo developer become a team champion:

| Trigger Moment | Product Response |
|---|---|
| A second developer needs access to the same MCP server | Prompt: "You've shared this server with a colleague. Upgrade to Team for proper access control." |
| Team needs deployment to shared staging/production | Cloud deployment adapters require Team tier |
| Security review requires audit logs | Audit log retention beyond 7 days requires Team tier |
| Multiple repos need centralized management | Workspace management requires Team tier |

### 12.3 Community Strategy

- **Contribution model:** Language plugin interface makes community contributions tractable (add Go support, Rust support, etc.)
- **Governance:** Benevolent dictator for v1; transition to steering committee at 20+ regular contributors
- **Channels:** GitHub Discussions (not Discord — lower maintenance burden for early stage)
- **Response SLA:** Issues responded to within 48 hours; PRs reviewed within 5 business days
- **First 10 contributors plan:** Seed example repos, language plugins, and deployment adapters as "good first issues"

---

## 13. Go-To-Market Strategy

### Phase 1: Developer Wedge

**Target:** Marcus-type developers, transitioning to Priya-type platform engineers
**Message:** "Turn your internal API folder into a secure MCP endpoint in 5 minutes"

**Channels:**
- Open-source GitHub repo with excellent README + animated demo GIF
- 3 example repos (Python scripts, OpenAPI spec, shell scripts)
- Hacker News launch post
- Product Hunt launch
- Reddit communities (r/mcp, r/LocalLLaMA, r/MachineLearning, r/devops)
- **YouTube:** 5-minute "watch me turn a folder into MCP tools" demo video
- **MCP Discord community:** Direct presence and support
- **Integration partnership:** Featured in Claude Desktop tool recommendations (pursue actively)
- Content: "OpenAPI to MCP in 5 minutes," "Secure MCP defaults checklist"

**Key metrics:** Weekly active deployments, time-to-first-tool-call, 30-day retention, developer NPS

### Phase 2: Enterprise Expansion

**Target:** Priya-type platform teams blocked on security, deployment, compliance
**Message:** "Bring your own tools. Run them with policy, audit, and cloud deployment."

**Channels:**
- Design partner pilots (3-5 platform teams — conversations start during Phase 0)
- Content targeting CISO/security teams: MCP security posture, audit trail requirements
- Conference presence: AI Engineer Summit, KubeCon (lightning talks)
- Integration with enterprise identity providers (Okta, Azure AD)

### Phase 3: Ecosystem Leverage

**Target:** SaaS vendors wanting AI-ready distribution
**Message:** "List in every AI client catalog from one deployment"

**Channels:**
- FolderMCP Registry: community tool bundle discovery
- `foldermcp register`: one-command publish to MCP Registry

---

## 14. Success Metrics

### Activation (per persona)

| Persona | Activation Event | Target |
|---|---|---|
| Marcus (Product Engineer) | First successful tool call from AI client | < 10 minutes |
| Priya (Platform Engineer) | Demo of scan-review-serve-deploy to team | < 2 hours |
| Dana (SRE) | 24-hour run in staging with clean logs | < 1 day |

### Core Experience

| Metric | Target | Baseline Today |
|---|---|---|
| Time to first AI tool call (Marcus) | < 10 minutes | ~24 hours[^2] |
| Cold start, FolderMCP runtime (excl. first tool invocation) | < 3 seconds | 5+ seconds[^1] |
| Token context for 4-server setup | < 8,000 | 60,000+[^11] |
| Auth failures (default insecure incidents) | 0 | Multiple documented[^10][^7] |
| Dependency resolution success (with requirements.txt) | > 95% | Not addressed by competitors |
| Dependency resolution success (inferred, common packages) | > 75% | Not addressed by competitors |
| Scan → review → serve completion rate | > 80% | N/A |

### Retention

| Metric | Target |
|---|---|
| 30-day retention (deployment still active) | > 40% |
| Monthly active tool invocations per deployment | > 50 |
| Organic inbound enterprise inquiries (Phase 2 leading indicator) | 5/month by month 4 |

### Quality Gates (Pre-Launch)

- MCP spec conformance tests: 100% pass
- Security review by third party: zero critical findings
- `foldermcp doctor` accuracy: correctly identifies 95%+ of real setup issues
- 24-hour continuous operation on Cloud Run, Docker: verified before v1 release
- README with animated demo + 3 working example repos: complete
- CI/CD pipeline (lint, test, build on Linux/macOS/Windows): green
- Test suite: > 60% coverage on core paths (scanner, deps, MCP server, approval)
- `SECURITY.md` with vulnerability reporting process: published

### Business

| Metric | 3 months | 6 months | 12 months |
|---|---|---|---|
| GitHub stars | 1,000 | 5,000 | 15,000 |
| Weekly active deployments | 500 | 5,000 | 25,000 |
| Enterprise design partners | 3 | 10 | 30 |
| Paid customers | 0 | 2 | 10 |

---

## 15. Risk Register

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| **Anthropic/platform vendors ship folder-to-MCP** | Medium | Very High | Move fast on approval layer + debugging studio (hard to copy). Differentiate on multi-client, deployment breadth, enterprise features. Monitor monthly. |
| Protocol specs change rapidly | High | High | Modular adapter architecture; version-pinning; automated spec tracking[^17] |
| Docker/Composio ship "bring your own folder" feature | Medium | High | Approval layer + debugging studio are hard to copy quickly |
| **FolderMCP itself has a security vulnerability** | Medium | Existential | Bug bounty program, penetration testing, responsible disclosure policy, incident response plan. Third-party security review before v1. |
| **MCP spec adds native auth/deployment primitives** | Medium | High | FolderMCP's value is the integrated workflow, not any single primitive. Stay close to spec working groups. |
| Security incident from misused tool exposure | Medium | Very High | Deny-all default; sandbox execution; signed manifests; security audit before v1[^24][^8] |
| Dependency introspection fails on complex projects | High | High | Start with curated mapping of top 500 packages; loud failures; graceful fallback to user-provided requirements.txt |
| **Dependency resolution installs unintended packages** (supply chain risk) | Medium | High | Show what will be installed, require confirmation. Never auto-install without user visibility. |
| Tool description quality too low for agent use | Medium | High | Editable review step; `@foldermcp.tool` decorator escape hatch; confidence labels |
| MCP picks a clear winner (serverless natively supported) | Low | Medium | Approval + debug + deploy layer still valuable even if MCP server creation gets easier |
| Supply chain attack via community MCP tools | High | High | HMAC tamper detection (v1), Sigstore signing (v2), sandboxed execution[^20][^7] |
| **Open-source community does not materialize** | Medium | High | Fallback GTM: direct outbound to design partners, paid pilots without community dependency |
| Monetization: enterprise features too slow to build | Medium | Medium | Design partners validate commercial layer before building; keep open-source core lean |

---

## 16. Roadmap

### Resource Plan

**Assumed team:** 3 engineers + 1 product/design

| Role | Count | Focus |
|---|---|---|
| Senior Backend Engineer (Go) | 1 | CLI, runtime, MCP server, sandbox |
| Senior Backend Engineer (Python/TS) | 1 | Introspectors, dependency resolution, tool execution |
| Full-Stack Engineer | 1 | Studio UI (Phase 1b), deployment adapters, doctor, connect |
| Product/Design | 1 | User research, UX design, docs, GTM |

### Phase 0: Validation Sprint (Days 1-14)

**Goal:** Confirm the product wedge is real before building.

**Deliverables:**
- 10-15 user interviews with platform engineers, AI infra teams, and product engineers
  - Include 3-5 prospective enterprise design partner candidates
  - Validate top source types and deployment targets
  - Validate pricing tier assumptions
- Clickable demo or CLI prototype (scan + review flow only)
- At least 2 team members dogfood the prototype on real internal tools
- Foundational engineering in parallel: file watcher, Python AST parser, CLI scaffold (low-risk, not dependent on validation)

**Exit criteria:**
- 70%+ of target users say the value proposition is compelling
- Top 2 source types and top 2 deployment targets confirmed
- At least 3 users want team/shared deployment (validates enterprise tier)
- CLI language confirmed (Go) and scaffold working

### Phase 1a: Walking Skeleton (Days 15-45)

**Goal:** 10 real users through the scan-review-serve loop.

**Scope:**
- [ ] Folder watcher + Python AST introspection
- [ ] OpenAPI spec parser → tool catalog
- [ ] Auto-dependency resolution (Python/uv only, curated mapping, `foldermcp.lock`)
- [ ] Scan scope configuration (include/exclude patterns)
- [ ] Review/approval CLI (batch-mode flags + optional TUI)
- [ ] MCP server: stdio only, deny-all default
- [ ] `foldermcp test <tool>` — CLI-based tool invocation
- [ ] `foldermcp status` — server state display
- [ ] `foldermcp doctor` — basic checks (5 top issues), no auto-fix
- [ ] `foldermcp catalog` — tool catalog display
- [ ] `foldermcp connect claude-desktop` — auto-configure Claude Desktop
- [ ] Docker deployment adapter
- [ ] `foldermcp.yaml` schema versioning (`version: 1`)
- [ ] Internal `.foldermcp/state.db` for runtime state
- [ ] README with animated demo + 1 working Python example repo
- [ ] GitHub Actions CI (lint, test, build on Linux/macOS/Windows)
- [ ] Core test suite (>60% coverage on scanner, deps, MCP server, approval)

**Exit criteria:**
- 10 real users complete scan → review → serve → first tool call
- Dependency resolution works for standard Python imports (>75% success without requirements.txt)
- `doctor` catches the top 5 common setup issues
- `connect claude-desktop` works on macOS and Windows

### Phase 1b: Completeness (Days 46-75)

**Scope:**
- [ ] TypeScript/JavaScript AST introspection + npm isolation
- [ ] Streamable HTTP transport (remote access)
- [ ] API key auth for team mode
- [ ] Self-signed TLS for localhost (mkcert)
- [ ] `foldermcp serve --verbose` payload logging
- [ ] `foldermcp logs` — audit log tailing
- [ ] `foldermcp ui` — web studio (payload inspector, test harness, token budget, catalog browser)
- [ ] Cloud Run deployment adapter
- [ ] A2A agent-card.json export
- [ ] `foldermcp doctor --fix` auto-remediation
- [ ] Shell script introspection
- [ ] `@foldermcp.tool` decorator for Python
- [ ] Output sanitization (size limits, secret redaction)
- [ ] Quickstart docs: Python folder, OpenAPI folder, shell scripts
- [ ] 3 example repos (Python, OpenAPI, shell)
- [ ] `SECURITY.md` + vulnerability reporting process
- [ ] `CONTRIBUTING.md` + setup instructions

**Exit criteria:**
- Full scan → review → serve → deploy loop works for Python, TypeScript, OpenAPI, and shell scripts
- Remote access (Streamable HTTP + API key) works end-to-end
- Studio UI shows live invocation payloads
- Ready for public open-source launch

### Launch (Day 75-80)

- [ ] Open-source release on GitHub
- [ ] Hacker News launch post
- [ ] Product Hunt launch
- [ ] YouTube demo video
- [ ] MCP Discord announcement
- [ ] MCP conformance test suite: 100% pass
- [ ] Third-party security review: zero critical findings

### Phase 2: Team Readiness (Days 81-150)

**Scope:**
- [ ] OAuth2/PKCE authentication
- [ ] AWS Lambda adapter
- [ ] Azure Functions adapter
- [ ] Semantic tool routing (local vector embeddings, opt-in)
- [ ] Workspace and profile management
- [ ] Structured audit logging with OTLP export
- [ ] `foldermcp connect cursor` + `foldermcp connect vscode`
- [ ] `foldermcp tunnel` — secure tunnel for local→public exposure
- [ ] CI/CD packaging support (GitHub Actions, GitLab CI)
- [ ] Let's Encrypt auto-TLS for production mode
- [ ] HMAC → Sigstore migration for manifest signing

**Exit criteria:**
- 3-5 design partners running in production or staging
- Enterprise auth (OAuth2) validated by at least 2 platform teams
- Repeat usage across multiple repos/workspaces

### Phase 3: Commercial Layer (Days 151-240)

**Scope:**
- [ ] Hosted enterprise control plane
- [ ] Team management, SSO, SAML
- [ ] Policy management UI
- [ ] SIEM export connectors
- [ ] A2A full runtime (HTTP + SSE, task lifecycle)
- [ ] ACP full runtime (REST + SSE, async)
- [ ] ACP/AGNTCY metadata exports
- [ ] FolderMCP Registry
- [ ] Go and Rust source introspection

**Exit criteria:**
- First paid enterprise pilots (2-3)
- Open-source community health: PRs from external contributors, issues response < 48h

---

## 17. Resolved Decisions

| Decision | Resolution | Rationale |
|---|---|---|
| CLI runtime language | **Go** | 1.3-1.5x velocity advantage over Rust for this class of project. Mature ecosystem (cobra, bubbletea, mcp-go). Larger contributor pool. Sandbox-critical subsystems can be rewritten in Rust later if needed. |
| Tool description generation | **Template-based (no LLM)** | `all-MiniLM-L6-v2` is an embedding model, not generative. Local LLM adds 2-4GB to install. Template-based (function name + params + docstring) covers 85%+ of well-documented functions. LLM-enhanced descriptions reserved for v2 as optional feature. |
| Dependency isolation scope | **Per-folder default** | Per-tool isolation creates N separate venvs (each ~300MB with numpy). Per-folder matches how developers organize code. Per-tool opt-in available for known conflicts. |
| MCP server implementation | **Wrap `mcp-go` SDK** | Building JSON-RPC 2.0 from scratch is 2-3 weeks. Wrapping existing SDK is 5-7 days. `mcp-go` provides transport + protocol; FolderMCP adds auth + approval + sandbox middleware. |
| Phase 1 web studio | **Deferred to Phase 1b** | 16-25 days of effort. CLI verbose logging + `foldermcp test` covers 80% of debugging need for early adopters. |
| Protocol exports in v1 | **A2A only** | ACP and AGNTCY are pre-1.0 and evolving. One stable export reduces maintenance by 66%. |
| Sandbox strategy | **Platform-tiered** | `seccomp` is Linux-only. Tier 1 (subprocess + resource limits) works everywhere. Tier 2 (seccomp) for Linux/containers. Tier 3 (Docker) for maximum isolation. |

---

## 18. Open Questions

1. **Offline LLM for descriptions (v2):** When adding optional LLM-enhanced descriptions in v2, what's the acceptable model size? A 1B parameter model (~2GB) provides decent quality but significant install footprint. Explore API-based option with clear opt-in.

2. **NLIP timing:** Ecma's Natural Language Interaction Protocol is still in progress. Preview adapter in Phase 3 or wait for finalized spec?[^35]

3. **ANP maturity threshold:** The Agent Network Protocol's W3C DID-based identity is promising but still early beta. When does it cross the threshold for a production adapter?[^36]

4. **Design partner naming:** Should the first 3-5 design partners be publicly named (validates enterprise credibility) or kept private (reduces pressure)? Decide at Phase 2 entry based on partner preference.

5. **Enterprise pricing model:** Per-seat, per-deployment, per-tool, or usage-based? Affects product architecture (e.g., per-deployment requires deployment counting). Validate during Phase 0 interviews.

---

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
│  MCP             │   │  A2A                        │
│  (v1 runtime)    │   │  (v1 metadata export only)  │
└───────────┬──────┘   └────────────────────────────┘
            │
┌───────────▼──────────────────────────────────────┐
│           FolderMCP Runtime v1                   │
│  Init → Scan → Review → Approve → Serve → Deploy │
│  Auth + Sandbox + Routing + Audit + Sanitizer    │
└───────────────────────┬──────────────────────────┘
                        │
         ┌──────────────▼──────────────┐
         │       Folder Contents        │
         │  .py  .ts  .sh  openapi.yaml │
         │  + auto-resolved deps        │
         └─────────────────────────────┘
```

## Appendix B: v1.0 → v2.0 → v2.1 Change Summary

| Section | v1.0 | v2.0 | v2.1 |
|---|---|---|---|
| Protocol scope | All 5 in v1 | MCP runtime; A2A/ACP metadata | MCP runtime; A2A metadata only (ACP/AGNTCY deferred) |
| Exposure model | Auto-publish | Approve before expose | Tiered review (dev bulk / team batch / prod individual) |
| Dependency handling | Not addressed | Per-tool auto-isolation | Per-folder default; per-tool opt-in; curated mapping; lockfile |
| Debugging | Not addressed | `foldermcp ui` in MVP | CLI verbose/test in 1a; UI in 1b |
| Diagnostics | Not addressed | `foldermcp doctor` | + `status`, `logs`, `catalog` commands |
| Sandbox | Not specified | seccomp + subprocess | Platform-tiered (all-platform → Linux → Docker) |
| Architecture | Not specified | Component map | + Lifecycle Controller, invocation pipeline, output sanitizer, plugin interface |
| Personas | Generic | Platform teams | Named personas with behavioral depth + SRE + Security |
| Performance | Not specified | <500ms cold start | <3 seconds (honest target) |
| Description gen | Not specified | LLM-assisted | Template-based (no LLM in v1) |
| CLI language | Not decided | Open question | Go (decided) |
| Resource plan | Not specified | Not specified | 3 engineers + 1 product |
| Pricing | Not specified | Open-core mentioned | Directional tiers defined |
| MVP scope | 12-month roadmap | 30-60-90 days | Phase 1a (30d) + 1b (30d) + launch |

---

## References

[^1]: [MCP in enterprise: real-world applications and challenges - Xenoss](https://xenoss.io/blog/mcp-model-context-protocol-enterprise-use-cases-implementation-challenges)
[^2]: [Agentic AI Statistics 2026 - Digital Applied](https://www.digitalapplied.com/blog/agentic-ai-statistics-2026-definitive-collection-150-data-points) *(Industry aggregator; figures are directionally useful but lack primary research methodology)*
[^3]: [MCP Server Discovery - ekamoira.com](https://www.ekamoira.com/blog/mcp-server-discovery-implement-well-known-mcp-json-2026-guide)
[^4]: [MCP server management pain - Reddit](https://www.reddit.com/r/mcp/comments/1mbljuu/anyone_else_finding_mcp_server_management_a_pain/)
[^5]: [VSCode Solved MCP's Biggest Pain Points - WorkOS](https://workos.com/blog/mcp-night-2-0-demo-recap-vscode-harald-kirschner)
[^6]: [MCP Marketplace Guide - Apigene](https://apigene.ai/blog/mcp-marketplace)
[^7]: [MCP Security: The Protocol Nobody Secured - CloudTweaks](https://cloudtweaks.com/2026/03/mcp-security-the-protocol-nobody-secured-before-shipping/)
[^8]: [OWASP MCP Top 10](https://owasp.org/www-project-mcp-top-10/)
[^9]: [Complicating Factors of MCP in Enterprise - Teleport](https://goteleport.com/blog/complicating-mcp-enterprise/)
[^10]: [MCP Defaults Will Betray You - CardinalOps](https://cardinalops.com/blog/mcp-defaults-hidden-dangers-of-remote-deployment/)
[^11]: [Solving MCP Tool Overload - Redis](https://redis.io/blog/from-reasoning-to-retrieval-solving-the-mcp-tool-overload-problem/)
[^15]: [AI Agents Market Report 2025-2030 - MarketsandMarkets](https://www.marketsandmarkets.com/Market-Reports/ai-agents-market-15761548.html)
[^16]: [How to Measure Developer GTM Success - Stateshift](https://blog.stateshift.com/how-to-measure-go-to-market-success-for-developer-audiences/)
[^17]: [MCP Roadmap](https://modelcontextprotocol.io/development/roadmap)
[^18]: [Docker MCP Catalog and Toolkit](https://docs.docker.com/ai/mcp-catalog-and-toolkit/)
[^20]: [MCP Supply Chain Security - MCPManager](https://mcpmanager.ai/blog/mcp-supply-chain-security/)
[^21]: [MCP Specification 2025-11-25](https://modelcontextprotocol.io/specification/2025-11-25)
[^22]: [Why MCP Deprecated SSE - fka.dev](https://blog.fka.dev/blog/2025-06-06-why-mcp-deprecated-sse-and-go-with-streamable-http/)
[^23]: [MCP llms-full.txt](https://modelcontextprotocol.io/llms-full.txt)
[^24]: [MCP Security CTO Action Plan - Obot](https://obot.ai/blog/mcp-security-cto-action-plan/)
[^25]: [Docker MCP Toolkit - Docker Blog](https://www.docker.com/blog/mcp-toolkit-mcp-servers-that-just-work/)
[^26]: [The Guide to MCP - Composio](https://composio.dev/content/the-guide-to-mcp-i-never-had)
[^27]: [Smithery vs Glama - nolist.ai](https://nolist.ai/compare/smithery-vs-glama)
[^28]: [MCP Gateway Alternatives - MintMCP](https://www.mintmcp.com/blog/portkey-with-mcp)
[^29]: [Best MCP Gateways - TrueFoundry](https://www.truefoundry.com/blog/best-mcp-gateways)
[^30]: [OpenAPI to MCP Codegen - GitHub](https://github.com/cnoe-io/openapi-mcp-codegen)
[^31]: [OpenAPI MCP Generator - GitHub](https://github.com/harsha-iiiv/openapi-mcp-generator)
[^35]: [AI Agents Protocol Snapshot - LinkedIn](https://www.linkedin.com/posts/arun-saraswat-44b3741_ai-agents-interoperability-activity-7363424883579514883-H_7l)
[^36]: [ANP Technical Specifications](https://agentnetworkprotocol.com/en/specs/)
