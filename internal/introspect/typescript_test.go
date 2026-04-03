package introspect

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestTypeScriptIntrospector_CanHandle(t *testing.T) {
	ts := &TypeScriptIntrospector{}

	tests := []struct {
		path string
		want bool
	}{
		{"tools.ts", true},
		{"utils.js", true},
		{"module.mjs", true},
		{"module.cjs", true},
		{"path/to/handler.ts", true},
		{"script.py", false},
		{"readme.md", false},
		{"types.d.ts", false},
		{"node_modules/lodash/index.js", false},
		{"path/node_modules/pkg/main.ts", false},
		{"package.json", false},
		{"", false},
	}

	for _, tt := range tests {
		got := ts.CanHandle(tt.path)
		if got != tt.want {
			t.Errorf("CanHandle(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestTypeScriptIntrospector_ExtractTools(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available:", err)
	}

	ts := &TypeScriptIntrospector{}
	filePath := filepath.Join(testdataDir(), "typescript_simple", "math_tools.ts")
	tools, err := ts.ExtractTools(context.Background(), filePath)
	if err != nil {
		t.Fatalf("ExtractTools() error: %v", err)
	}

	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}

	// Check first tool: addNumbers
	add := tools[0]
	if add.Name != "addNumbers" {
		t.Errorf("expected name 'addNumbers', got %q", add.Name)
	}
	if add.Description != "Add two numbers together." {
		t.Errorf("unexpected description: %q", add.Description)
	}
	if add.Language != "typescript" {
		t.Errorf("expected language 'typescript', got %q", add.Language)
	}
	if add.Risk != "read_only" {
		t.Errorf("expected risk 'read_only', got %q", add.Risk)
	}

	// Validate JSON schema is valid JSON
	var schema map[string]interface{}
	if err := json.Unmarshal([]byte(add.InputSchema), &schema); err != nil {
		t.Errorf("InputSchema is not valid JSON: %v", err)
	}

	// Check schema has correct properties
	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("schema missing 'properties'")
	}
	aProp, ok := props["a"].(map[string]interface{})
	if !ok {
		t.Fatal("schema missing property 'a'")
	}
	if aProp["type"] != "number" {
		t.Errorf("expected 'a' type 'number', got %v", aProp["type"])
	}

	// Check second tool: reverseString
	rev := tools[1]
	if rev.Name != "reverseString" {
		t.Errorf("expected name 'reverseString', got %q", rev.Name)
	}
	if rev.Description != "Reverse a string." {
		t.Errorf("unexpected description: %q", rev.Description)
	}
}

func TestTypeScriptIntrospector_ExtractTools_JSCommonJS(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available:", err)
	}

	ts := &TypeScriptIntrospector{}
	filePath := filepath.Join(testdataDir(), "typescript_simple", "api_client.js")
	tools, err := ts.ExtractTools(context.Background(), filePath)
	if err != nil {
		t.Fatalf("ExtractTools() error: %v", err)
	}

	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}

	fetch := tools[0]
	if fetch.Name != "fetchUser" {
		t.Errorf("expected name 'fetchUser', got %q", fetch.Name)
	}
	if fetch.Description != "Fetch user data from the API." {
		t.Errorf("unexpected description: %q", fetch.Description)
	}
	if fetch.Language != "javascript" {
		t.Errorf("expected language 'javascript', got %q", fetch.Language)
	}
}

func TestTypeScriptIntrospector_InferDependencies(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available:", err)
	}

	ts := &TypeScriptIntrospector{}
	filePath := filepath.Join(testdataDir(), "typescript_simple", "api_client.js")
	deps, err := ts.InferDependencies(context.Background(), filePath)
	if err != nil {
		t.Fatalf("InferDependencies() error: %v", err)
	}

	// Should find axios and lodash (not relative imports)
	if len(deps) != 2 {
		t.Fatalf("expected 2 dependencies, got %d: %+v", len(deps), deps)
	}

	depNames := make(map[string]bool)
	for _, d := range deps {
		depNames[d.ImportName] = true
	}

	if !depNames["axios"] {
		t.Error("expected dependency 'axios'")
	}
	if !depNames["lodash"] {
		t.Error("expected dependency 'lodash'")
	}
}
