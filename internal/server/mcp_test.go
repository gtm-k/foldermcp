package server

import (
	"testing"

	"github.com/gtm-k/foldermcp/internal/sandbox"
	"github.com/gtm-k/foldermcp/internal/state"
)

func TestNewMCPServer_OnlyRegistersEnabledTools(t *testing.T) {
	tools := []state.Tool{
		{
			Name:        "enabled_tool",
			SourceFile:  "tools/enabled.py",
			Description: "An enabled tool",
			InputSchema: `{"type":"object","properties":{"x":{"type":"string"}}}`,
			Risk:        "low",
			State:       "enabled",
			DepState:    "ready",
		},
		{
			Name:        "disabled_tool",
			SourceFile:  "tools/disabled.py",
			Description: "A disabled tool",
			InputSchema: `{"type":"object","properties":{}}`,
			Risk:        "low",
			State:       "disabled",
			DepState:    "ready",
		},
		{
			Name:        "pending_tool",
			SourceFile:  "tools/pending.py",
			Description: "A pending tool",
			InputSchema: `{"type":"object","properties":{}}`,
			Risk:        "low",
			State:       "pending",
			DepState:    "ready",
		},
	}

	srv, err := NewMCPServer("test-server", "0.0.1", tools, nil, nil, &MCPServerConfig{})
	if err != nil {
		t.Fatalf("NewMCPServer: %v", err)
	}

	if srv.enabledToolCount != 1 {
		t.Errorf("enabledToolCount = %d, want 1", srv.enabledToolCount)
	}
}

func TestNewMCPServer_RegistersRequiresConfirmation(t *testing.T) {
	tools := []state.Tool{
		{
			Name:        "confirm_tool",
			SourceFile:  "tools/confirm.py",
			Description: "A tool requiring confirmation",
			InputSchema: `{"type":"object","properties":{"action":{"type":"string"}}}`,
			Risk:        "high",
			State:       "requires_confirmation",
			DepState:    "ready",
		},
		{
			Name:        "enabled_tool",
			SourceFile:  "tools/enabled.py",
			Description: "An enabled tool",
			InputSchema: `{"type":"object","properties":{}}`,
			Risk:        "low",
			State:       "enabled",
			DepState:    "ready",
		},
		{
			Name:        "disabled_tool",
			SourceFile:  "tools/disabled.py",
			Description: "A disabled tool",
			InputSchema: `{"type":"object","properties":{}}`,
			Risk:        "low",
			State:       "disabled",
			DepState:    "ready",
		},
	}

	srv, err := NewMCPServer("test-server", "0.0.1", tools, nil, nil, &MCPServerConfig{})
	if err != nil {
		t.Fatalf("NewMCPServer: %v", err)
	}

	// Both "enabled" and "requires_confirmation" should be registered.
	if srv.enabledToolCount != 2 {
		t.Errorf("enabledToolCount = %d, want 2", srv.enabledToolCount)
	}
}

func TestNewMCPServer_NilConfig(t *testing.T) {
	tools := []state.Tool{
		{
			Name:        "tool1",
			SourceFile:  "tools/t.py",
			Description: "A tool",
			InputSchema: `{"type":"object","properties":{}}`,
			Risk:        "low",
			State:       "enabled",
			DepState:    "ready",
		},
	}

	// Passing nil config should not panic — it should use defaults.
	srv, err := NewMCPServer("test-server", "0.0.1", tools, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewMCPServer with nil config: %v", err)
	}

	if srv.enabledToolCount != 1 {
		t.Errorf("enabledToolCount = %d, want 1", srv.enabledToolCount)
	}
}

func TestNewMCPServer_EmptyTools(t *testing.T) {
	srv, err := NewMCPServer("test-server", "0.0.1", nil, nil, nil, &MCPServerConfig{})
	if err != nil {
		t.Fatalf("NewMCPServer: %v", err)
	}

	if srv.enabledToolCount != 0 {
		t.Errorf("enabledToolCount = %d, want 0", srv.enabledToolCount)
	}
}

func TestNewMCPServer_ToolMapPopulated(t *testing.T) {
	tools := []state.Tool{
		{
			Name:        "tool_a",
			SourceFile:  "tools/a.py",
			Description: "Tool A",
			InputSchema: `{"type":"object","properties":{}}`,
			Risk:        "low",
			State:       "enabled",
			DepState:    "ready",
		},
		{
			Name:        "tool_b",
			SourceFile:  "tools/b.py",
			Description: "Tool B",
			InputSchema: `{"type":"object","properties":{}}`,
			Risk:        "medium",
			State:       "requires_confirmation",
			DepState:    "ready",
		},
	}

	srv, err := NewMCPServer("test-server", "0.0.1", tools, nil, nil, &MCPServerConfig{})
	if err != nil {
		t.Fatalf("NewMCPServer: %v", err)
	}

	if _, ok := srv.tools["tool_a"]; !ok {
		t.Error("tool_a should be in the tools map")
	}
	if _, ok := srv.tools["tool_b"]; !ok {
		t.Error("tool_b should be in the tools map")
	}
}

func TestNewMCPServer_WithExecutor(t *testing.T) {
	// Create server with a real executor, verify tool handler works.
	tools := []state.Tool{
		{
			Name:        "test_tool",
			SourceFile:  "test.py",
			Description: "Test",
			InputSchema: `{"type":"object"}`,
			State:       "enabled",
			DepState:    "resolved",
		},
	}
	exec := sandbox.NewExecutor(sandbox.ExecutorConfig{TimeoutSeconds: 5})
	san := sandbox.NewSanitizer(1024)

	s, err := NewMCPServer("test", "0.1.0", tools, nil, nil, &MCPServerConfig{
		Executor:  exec,
		Sanitizer: san,
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.enabledToolCount != 1 {
		t.Errorf("expected 1, got %d", s.enabledToolCount)
	}
}

func TestNewMCPServer_DisabledToolsExcluded(t *testing.T) {
	tools := []state.Tool{
		{Name: "a", State: "enabled", DepState: "resolved", InputSchema: `{}`},
		{Name: "b", State: "disabled", DepState: "resolved", InputSchema: `{}`},
		{Name: "c", State: "pending", DepState: "resolved", InputSchema: `{}`},
		{Name: "d", State: "requires_confirmation", DepState: "resolved", InputSchema: `{}`},
	}
	s, _ := NewMCPServer("test", "0.1.0", tools, nil, nil, nil)
	// Should have 2: enabled + requires_confirmation
	if s.enabledToolCount != 2 {
		t.Errorf("expected 2 enabled, got %d", s.enabledToolCount)
	}
}

func TestToolError_Error(t *testing.T) {
	e := &ToolError{Code: ErrToolExecutionFailed, Message: "test error", Data: "detail"}
	if e.Error() != "test error" {
		t.Errorf("expected 'test error', got %q", e.Error())
	}
}

func TestToolError_AllCodes(t *testing.T) {
	tests := []struct {
		code int
		name string
	}{
		{ErrToolExecutionFailed, "execution_failed"},
		{ErrToolTimeout, "timeout"},
		{ErrToolDependencyError, "dependency_error"},
		{ErrToolAuthDenied, "auth_denied"},
		{ErrToolConfirmRequired, "confirm_required"},
	}
	for _, tt := range tests {
		e := &ToolError{Code: tt.code, Message: tt.name}
		if e.Error() != tt.name {
			t.Errorf("code %d: expected %q, got %q", tt.code, tt.name, e.Error())
		}
	}
}
