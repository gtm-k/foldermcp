# Security Policy

## Reporting Vulnerabilities

If you discover a security vulnerability in FolderMCP, please report it
responsibly. Do NOT open a public GitHub issue for security vulnerabilities.

**Preferred methods:**

- **GitHub Security Advisories:** Create a GitHub Security Advisory at
  [github.com/gtm-k/foldermcp/security/advisories](https://github.com/gtm-k/foldermcp/security/advisories).

Include the following in your report:

- Description of the vulnerability.
- Steps to reproduce or a proof-of-concept.
- Affected versions, if known.
- Suggested fix, if any.

## Response Timeline

| Stage | Target |
|-------|--------|
| Acknowledgment | Within 48 hours of report |
| Initial assessment | Within 5 business days |
| Fix for critical issues | Within 30 days |
| Public disclosure | After fix is released and users have had time to update |

We will keep reporters informed of progress throughout the process.

## Scope

The following areas are in scope for security reports:

- **CLI** -- Command injection, path traversal, or privilege escalation via
  FolderMCP commands.
- **Runtime and sandbox** -- Sandbox escapes, unauthorized filesystem access,
  or execution of unapproved tools.
- **Authentication and authorization** -- Bypassing tool review or approval
  mechanisms.
- **Secrets handling** -- Exposure of credentials, API keys, or sensitive
  configuration values through logs, output, or error messages.
- **State store** -- SQL injection or data corruption in the SQLite state store.

Out of scope:

- Vulnerabilities in upstream dependencies (report those to the relevant
  project, though we appreciate a heads-up).
- Denial of service attacks that require local access (the CLI is a local tool).

## Security Model

FolderMCP is designed with the following security principles:

### Deny by default

All discovered tools start in a `pending` state. No tool can be invoked until
it has been explicitly approved through the `review` command. In production
mode, each tool is reviewed individually.

### Sandboxed execution

Tools run in isolated subprocesses with:

- Configurable execution timeouts (default: 30 seconds).
- Output size limits (default: 100 KB).
- No access to the parent process environment beyond what is explicitly
  configured.

### Output sanitization

All tool output is validated and truncated before being returned to the MCP
client. This reduces the risk of prompt injection or exfiltration through
oversized responses.

### Audit logging

Every tool invocation is recorded in a local audit log at
`.foldermcp/audit.log`, including:

- Timestamp.
- Tool name and parameters.
- Execution result (success or error).
- Duration.

### Risk classification

Each tool is assigned a risk level during introspection:

- **read_only** -- No side effects.
- **state_changing** -- Modifies external state.
- **destructive** -- Irreversible operations.

High-risk tools can be configured to require human confirmation before each
invocation.

## API Key Management

In team mode, FolderMCP generates an API key stored at `.foldermcp/api.key`.
To rotate the key:
1. Stop the server
2. Delete `.foldermcp/api.key`
3. Restart — a new key will be generated automatically

Future versions will support key rotation without restart.
