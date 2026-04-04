package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/foldermcp/foldermcp/internal/audit"
	"github.com/foldermcp/foldermcp/internal/sandbox"
	"github.com/foldermcp/foldermcp/internal/state"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// MCPServer wraps a mcp-go MCPServer and provides tool registration,
// execution, sanitization, and audit logging.
type MCPServer struct {
	server           *server.MCPServer
	tools            map[string]state.Tool
	executor         *sandbox.Executor
	sanitizer        *sandbox.Sanitizer
	logger           *audit.Logger
	enabledToolCount int
}

// MCPServerConfig holds optional dependencies for the MCP server.
// Nil fields are handled gracefully (e.g., no-op audit logging, stub execution).
type MCPServerConfig struct {
	Executor  *sandbox.Executor
	Sanitizer *sandbox.Sanitizer
	Logger    *audit.Logger
}

// NewMCPServer creates an MCP server, registering only tools whose State is
// "enabled" or "requires_confirmation". Tools with State "disabled" or
// "pending" are skipped entirely and will not appear in tools/list.
func NewMCPServer(name, version string, tools []state.Tool, cfg *MCPServerConfig) (*MCPServer, error) {
	if cfg == nil {
		cfg = &MCPServerConfig{}
	}

	mcpSrv := server.NewMCPServer(name, version, server.WithToolCapabilities(true))

	ms := &MCPServer{
		server:    mcpSrv,
		tools:     make(map[string]state.Tool),
		executor:  cfg.Executor,
		sanitizer: cfg.Sanitizer,
		logger:    cfg.Logger,
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

	return ms, nil
}

// makeToolHandler returns a ToolHandlerFunc for the given tool. It checks
// state, dep_state, invokes execution, audits, and sanitizes output.
func (ms *MCPServer) makeToolHandler(t state.Tool) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
		if err := ms.logger.Log(toolName, "invoke", sanitizedParams, "", resultStatus); err != nil {
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
