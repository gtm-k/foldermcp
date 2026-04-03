// Package server provides the MCP server implementation using mcp-go.
package server

// Structured error codes for tool execution failures. These follow the
// JSON-RPC error code convention with application-specific codes in the
// -32000 to -32099 range.
const (
	ErrToolExecutionFailed = -32000
	ErrToolTimeout         = -32001
	ErrToolDependencyError = -32002
	ErrToolAuthDenied      = -32003
	ErrToolConfirmRequired = -32004
)

// ToolError represents a structured error returned from tool execution.
type ToolError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data,omitempty"`
}

// Error implements the error interface.
func (e *ToolError) Error() string { return e.Message }
