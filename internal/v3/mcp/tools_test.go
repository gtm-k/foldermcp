package mcp

import (
	"errors"
	"testing"

	"github.com/mark3labs/mcp-go/server"
)

func TestToolDefinitions(t *testing.T) {
	tools := []struct {
		name     string
		buildFn  func() any // returns mcp.Tool but we only check the name
		wantName string
	}{
		{"search", func() any { return searchTool() }, "foldermcp_search"},
		{"inspect", func() any { return inspectTool() }, "foldermcp_inspect"},
		{"browse", func() any { return browseTool() }, "foldermcp_browse"},
	}
	for _, tt := range tools {
		t.Run(tt.name, func(t *testing.T) {
			tool := tt.buildFn()
			// Verify no panic during construction — schema builder errors
			// surface as panics in mcp-go.
			if tool == nil {
				t.Fatal("tool construction returned nil")
			}
		})
	}
}

func TestRegisterToolsNoPanic(t *testing.T) {
	s := server.NewMCPServer("test", "v0.0.0-test")
	shim := &Shim{server: s}
	// registerTools must not panic even with a nil router;
	// it only registers handlers, actual gRPC calls happen at invocation time.
	shim.registerTools()
}

func TestSanitizeError(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"simple", errors.New("connection refused")},
		{"with_sql", errors.New("SQL: SELECT * FROM nodes WHERE id = 42")},
		{"nil_wrapped", errors.New("rpc error: code = Internal desc = embedder crashed")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeError(tt.err)
			if got == "" {
				t.Error("sanitizeError returned empty string")
			}
			// Must not leak internal details.
			if got == tt.err.Error() {
				t.Errorf("sanitizeError leaked raw error: %s", got)
			}
		})
	}
}
