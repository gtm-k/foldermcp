package server

import (
	"testing"

	"github.com/foldermcp/foldermcp/internal/state"
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

	srv, err := NewMCPServer("test-server", "0.0.1", tools, &MCPServerConfig{})
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

	srv, err := NewMCPServer("test-server", "0.0.1", tools, &MCPServerConfig{})
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
	srv, err := NewMCPServer("test-server", "0.0.1", tools, nil)
	if err != nil {
		t.Fatalf("NewMCPServer with nil config: %v", err)
	}

	if srv.enabledToolCount != 1 {
		t.Errorf("enabledToolCount = %d, want 1", srv.enabledToolCount)
	}
}

func TestNewMCPServer_EmptyTools(t *testing.T) {
	srv, err := NewMCPServer("test-server", "0.0.1", nil, &MCPServerConfig{})
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

	srv, err := NewMCPServer("test-server", "0.0.1", tools, &MCPServerConfig{})
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
