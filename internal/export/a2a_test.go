package export

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/foldermcp/foldermcp/internal/state"
)

func TestGenerateAgentCard(t *testing.T) {
	tools := []state.Tool{
		{Name: "add_numbers", Description: "Add two numbers together"},
		{Name: "deploy", Description: "Deploy the application to staging"},
		{Name: "health_check", Description: "Check service health status"},
	}

	card := GenerateAgentCard("my-project", "1.0.0", "http://localhost:3000", tools)

	if card.Name != "my-project" {
		t.Errorf("expected name 'my-project', got %q", card.Name)
	}
	if card.Version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got %q", card.Version)
	}
	if card.URL != "http://localhost:3000" {
		t.Errorf("expected URL 'http://localhost:3000', got %q", card.URL)
	}
	if len(card.Capabilities) != 3 {
		t.Fatalf("expected 3 capabilities, got %d", len(card.Capabilities))
	}

	// Verify capability names match tool names.
	expectedNames := []string{"add_numbers", "deploy", "health_check"}
	for i, cap := range card.Capabilities {
		if cap.Name != expectedNames[i] {
			t.Errorf("capability[%d]: expected name %q, got %q", i, expectedNames[i], cap.Name)
		}
	}

	// Verify descriptions are carried through.
	if card.Capabilities[0].Description != "Add two numbers together" {
		t.Errorf("unexpected description: %q", card.Capabilities[0].Description)
	}
}

func TestGenerateAgentCard_Empty(t *testing.T) {
	card := GenerateAgentCard("empty", "0.1.0", "http://localhost:3000", nil)

	if len(card.Capabilities) != 0 {
		t.Errorf("expected 0 capabilities for nil tools, got %d", len(card.Capabilities))
	}
}

func TestWriteAgentCard(t *testing.T) {
	tmpDir := t.TempDir()

	card := &AgentCard{
		Name:        "test-agent",
		Description: "A test agent",
		URL:         "http://localhost:3000",
		Version:     "0.1.0",
		Capabilities: []Capability{
			{Name: "greet", Description: "Say hello"},
			{Name: "compute", Description: "Run a computation"},
		},
	}

	if err := WriteAgentCard(tmpDir, card); err != nil {
		t.Fatalf("WriteAgentCard() error: %v", err)
	}

	// Read back the file.
	outPath := filepath.Join(tmpDir, "agent-card.json")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read agent-card.json: %v", err)
	}

	// Parse back and verify structure.
	var parsed AgentCard
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal agent-card.json: %v", err)
	}

	if parsed.Name != "test-agent" {
		t.Errorf("expected name 'test-agent', got %q", parsed.Name)
	}
	if parsed.Version != "0.1.0" {
		t.Errorf("expected version '0.1.0', got %q", parsed.Version)
	}
	if len(parsed.Capabilities) != 2 {
		t.Fatalf("expected 2 capabilities, got %d", len(parsed.Capabilities))
	}
	if parsed.Capabilities[0].Name != "greet" {
		t.Errorf("expected first capability 'greet', got %q", parsed.Capabilities[0].Name)
	}
	if parsed.Capabilities[1].Name != "compute" {
		t.Errorf("expected second capability 'compute', got %q", parsed.Capabilities[1].Name)
	}
}
