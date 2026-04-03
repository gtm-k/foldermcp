package introspect

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestShellIntrospector_CanHandle(t *testing.T) {
	s := &ShellIntrospector{}

	tests := []struct {
		path string
		want bool
	}{
		{"deploy.sh", true},
		{"scripts/build.sh", true},
		{"run.bash", true},
		{"script.py", false},
		{"readme.md", false},
		{"Makefile", false},
		{"script.SH", false}, // case-sensitive
		{"", false},
	}

	for _, tt := range tests {
		got := s.CanHandle(tt.path)
		if got != tt.want {
			t.Errorf("CanHandle(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestShellIntrospector_ExtractTools(t *testing.T) {
	s := &ShellIntrospector{}
	filePath := filepath.Join(testdataDir(), "shell_simple", "deploy.sh")
	tools, err := s.ExtractTools(filePath)
	if err != nil {
		t.Fatalf("ExtractTools() error: %v", err)
	}

	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}

	tool := tools[0]

	if tool.Name != "deploy" {
		t.Errorf("expected name 'deploy', got %q", tool.Name)
	}

	if !strings.Contains(tool.Description, "Deploy") {
		t.Errorf("expected description to contain 'Deploy', got %q", tool.Description)
	}

	if tool.Risk != "side_effects" {
		t.Errorf("expected risk 'side_effects', got %q", tool.Risk)
	}

	if tool.Language != "shell" {
		t.Errorf("expected language 'shell', got %q", tool.Language)
	}

	// Validate InputSchema is valid JSON with the expected shape.
	var schema map[string]interface{}
	if err := json.Unmarshal([]byte(tool.InputSchema), &schema); err != nil {
		t.Fatalf("InputSchema is not valid JSON: %v", err)
	}
	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("schema missing 'properties'")
	}
	argsProp, ok := props["args"].(map[string]interface{})
	if !ok {
		t.Fatal("schema missing property 'args'")
	}
	if argsProp["type"] != "string" {
		t.Errorf("expected 'args' type 'string', got %v", argsProp["type"])
	}
}

func TestShellIntrospector_ExtractTools_HealthCheck(t *testing.T) {
	s := &ShellIntrospector{}
	filePath := filepath.Join(testdataDir(), "shell_simple", "health_check.sh")
	tools, err := s.ExtractTools(filePath)
	if err != nil {
		t.Fatalf("ExtractTools() error: %v", err)
	}

	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}

	tool := tools[0]

	if tool.Name != "health_check" {
		t.Errorf("expected name 'health_check', got %q", tool.Name)
	}

	if !strings.Contains(tool.Description, "health") {
		t.Errorf("expected description to contain 'health', got %q", tool.Description)
	}
}

func TestShellIntrospector_InferDependencies(t *testing.T) {
	s := &ShellIntrospector{}
	filePath := filepath.Join(testdataDir(), "shell_simple", "deploy.sh")
	deps, err := s.InferDependencies(filePath)
	if err != nil {
		t.Fatalf("InferDependencies() error: %v", err)
	}

	if deps != nil {
		t.Errorf("expected nil dependencies, got %+v", deps)
	}
}
