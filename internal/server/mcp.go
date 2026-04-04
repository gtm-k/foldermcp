package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/gtm-k/foldermcp/internal/audit"
	"github.com/gtm-k/foldermcp/internal/sandbox"
	"github.com/gtm-k/foldermcp/internal/state"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// MCPServer wraps a mcp-go MCPServer and provides tool registration,
// execution, sanitization, and audit logging.
type MCPServer struct {
	server           *server.MCPServer
	tools            map[string]state.Tool
	store            *state.Store
	executor         *sandbox.Executor
	sanitizer        *sandbox.Sanitizer
	logger           *audit.Logger
	httpServer           *http.Server
	transport            string // "stdio" or "http"
	enabledToolCount     int
	enabledResourceCount int
}

// MCPServerConfig holds optional dependencies for the MCP server.
// Nil fields are handled gracefully (e.g., no-op audit logging, stub execution).
type MCPServerConfig struct {
	Executor  *sandbox.Executor
	Sanitizer *sandbox.Sanitizer
	Logger    *audit.Logger
	Transport string // "stdio" or "http"; used as caller identity in audit logs
}

// NewMCPServer creates an MCP server, registering only tools whose State is
// "enabled" or "requires_confirmation". Tools with State "disabled" or
// "pending" are skipped entirely and will not appear in tools/list.
// Resources with State "enabled" are registered as MCP resources.
// The store parameter may be nil (e.g. in tests); when non-nil, tool state
// is re-read from the store on each invocation for live updates.
func NewMCPServer(name, version string, tools []state.Tool, resources []state.Resource, store *state.Store, cfg *MCPServerConfig) (*MCPServer, error) {
	if cfg == nil {
		cfg = &MCPServerConfig{}
	}

	// Enable resource capabilities if there are any enabled resources.
	hasResources := false
	for _, r := range resources {
		if r.State == "enabled" {
			hasResources = true
			break
		}
	}

	var opts []server.ServerOption
	opts = append(opts, server.WithToolCapabilities(true))
	if hasResources {
		opts = append(opts, server.WithResourceCapabilities(false, false))
	}

	mcpSrv := server.NewMCPServer(name, version, opts...)

	transport := cfg.Transport
	if transport == "" {
		transport = "stdio"
	}

	ms := &MCPServer{
		server:    mcpSrv,
		tools:     make(map[string]state.Tool),
		store:     store,
		executor:  cfg.Executor,
		sanitizer: cfg.Sanitizer,
		logger:    cfg.Logger,
		transport: transport,
	}

	for _, t := range tools {
		if t.State != "enabled" && t.State != "requires_confirmation" {
			continue
		}

		// Determine the raw input schema. If the tool has one, use it;
		// otherwise fall back to a minimal object schema.
		rawSchema := json.RawMessage(t.InputSchema)
		if len(t.InputSchema) == 0 {
			rawSchema = json.RawMessage(`{"type":"object","properties":{}}`)
		}

		mcpTool := mcp.NewToolWithRawSchema(t.Name, t.Description, rawSchema)

		ms.tools[t.Name] = t
		ms.enabledToolCount++

		// Capture loop variable for handler closure.
		toolCopy := t
		mcpSrv.AddTool(mcpTool, ms.makeToolHandler(toolCopy))
	}

	// Register enabled resources.
	for _, r := range resources {
		if r.State != "enabled" {
			continue
		}

		uri := "file://" + strings.ReplaceAll(r.FilePath, "\\", "/")
		mcpResource := mcp.NewResource(
			uri,
			r.Name,
			mcp.WithResourceDescription(fmt.Sprintf("%s (%s, %d bytes)", r.ResourceType, r.MimeType, r.SizeBytes)),
			mcp.WithMIMEType(r.MimeType),
		)

		resCopy := r
		mcpSrv.AddResource(mcpResource, ms.makeResourceHandler(resCopy))
		ms.enabledResourceCount++
	}

	return ms, nil
}

// makeResourceHandler returns a ResourceHandlerFunc that reads the file and
// returns its content as text (for text files) or base64 blob (for binary files).
func (ms *MCPServer) makeResourceHandler(r state.Resource) server.ResourceHandlerFunc {
	return func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		data, err := os.ReadFile(r.FilePath)
		if err != nil {
			return nil, fmt.Errorf("read resource %q: %w", r.Name, err)
		}

		uri := "file://" + strings.ReplaceAll(r.FilePath, "\\", "/")

		if isTextMime(r.MimeType) {
			return []mcp.ResourceContents{
				mcp.TextResourceContents{
					URI:      uri,
					MIMEType: r.MimeType,
					Text:     string(data),
				},
			}, nil
		}

		// Binary content — return base64-encoded.
		return []mcp.ResourceContents{
			mcp.BlobResourceContents{
				URI:      uri,
				MIMEType: r.MimeType,
				Blob:     base64.StdEncoding.EncodeToString(data),
			},
		}, nil
	}
}

// isTextMime returns true if the MIME type represents text content.
func isTextMime(mime string) bool {
	textMimes := map[string]bool{
		"text/plain":    true,
		"text/markdown": true,
		"text/csv":      true,
		"text/yaml":     true,
		"text/toml":     true,
		"text/html":     true,
		"image/svg+xml": true,
		"application/json": true,
	}
	return textMimes[mime]
}

// EnabledResourceCount returns the number of resources registered in the MCP server.
func (ms *MCPServer) EnabledResourceCount() int {
	return ms.enabledResourceCount
}

// makeToolHandler returns a ToolHandlerFunc for the given tool. It checks
// state, dep_state, invokes execution, audits, and sanitizes output.
// When a store is available, tool state is re-read on each invocation to
// pick up live changes (e.g. a tool being disabled via CLI).
func (ms *MCPServer) makeToolHandler(t state.Tool) server.ToolHandlerFunc {
	toolName := t.Name
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Re-read current tool state from the store if available.
		if ms.store != nil {
			fresh, err := ms.store.GetTool(toolName)
			if err != nil || fresh == nil {
				return nil, &ToolError{
					Code:    ErrToolAuthDenied,
					Message: fmt.Sprintf("tool %q not found", toolName),
				}
			}
			t = *fresh
		}

		// 1. Check tool state — if disabled at call time, deny.
		if t.State == "disabled" {
			return nil, &ToolError{
				Code:    ErrToolAuthDenied,
				Message: fmt.Sprintf("tool %q is disabled", t.Name),
			}
		}

		// 2. Check dependency state.
		if t.DepState == "resolving" {
			return nil, &ToolError{
				Code:    ErrToolDependencyError,
				Message: fmt.Sprintf("tool %q dependencies are still resolving", t.Name),
			}
		}
		if t.DepState == "failed" {
			return nil, &ToolError{
				Code:    ErrToolDependencyError,
				Message: fmt.Sprintf("tool %q dependency resolution failed", t.Name),
			}
		}

		// Marshal arguments to JSON for execution and logging.
		args := request.GetArguments()
		argsJSON, err := json.Marshal(args)
		if err != nil {
			argsJSON = []byte("{}")
		}

		var resultText string

		if ms.executor != nil {
			// 3. Execute via sandbox executor.
			result, err := ms.executor.RunPythonFile(
				ctx,
				t.SourceFile,
				t.Name, // function name matches tool name
				string(argsJSON),
				"", // venvPath — can be extended later
				nil,
			)
			if err != nil {
				ms.logInvocation(t.Name, string(argsJSON), "error")
				return nil, &ToolError{
					Code:    ErrToolExecutionFailed,
					Message: fmt.Sprintf("tool %q execution failed: %v", t.Name, err),
				}
			}
			if result.ExitCode != 0 {
				ms.logInvocation(t.Name, string(argsJSON), "error")
				return nil, &ToolError{
					Code:    ErrToolExecutionFailed,
					Message: fmt.Sprintf("tool %q exited with code %d: %s", t.Name, result.ExitCode, result.Stderr),
				}
			}
			resultText = result.Stdout
		} else {
			// Stub mode: no executor — return a placeholder showing the call.
			resultText = fmt.Sprintf("[stub] tool=%s args=%s", t.Name, string(argsJSON))
		}

		// 4. Audit log the invocation.
		ms.logInvocation(t.Name, string(argsJSON), "success")

		// 5. Sanitize output.
		if ms.sanitizer != nil {
			resultText = ms.sanitizer.Sanitize(resultText)
		}

		// 6. Return result as text content.
		return mcp.NewToolResultText(resultText), nil
	}
}

// logInvocation logs a tool call via the audit logger, if one is configured.
// Parameters are sanitized before logging to prevent secret leakage.
func (ms *MCPServer) logInvocation(toolName, params, resultStatus string) {
	if ms.logger != nil {
		sanitizedParams := params
		if ms.sanitizer != nil {
			sanitizedParams = ms.sanitizer.SanitizeParams(params)
		}
		if err := ms.logger.Log(toolName, "invoke", sanitizedParams, ms.transport, resultStatus); err != nil {
			fmt.Fprintf(os.Stderr, "audit log error: %v\n", err)
		}
	}
}

// EnabledToolCount returns the number of tools registered in the MCP server
// (those with state "enabled" or "requires_confirmation").
func (ms *MCPServer) EnabledToolCount() int {
	return ms.enabledToolCount
}

// ServeStdio starts the MCP server on stdin/stdout using the stdio transport.
func (ms *MCPServer) ServeStdio() error {
	return server.ServeStdio(ms.server)
}
