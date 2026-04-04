package introspect

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func testdataDir() string {
	// Go up from internal/introspect to project root, then into testdata.
	return filepath.Join("..", "..", "testdata")
}

func TestPythonIntrospector_CanHandle(t *testing.T) {
	p := &PythonIntrospector{}

	tests := []struct {
		path string
		want bool
	}{
		{"tools.py", true},
		{"path/to/script.py", true},
		{"module.ts", false},
		{"readme.md", false},
		{"script.PY", false}, // case-sensitive
		{"", false},
	}

	for _, tt := range tests {
		got := p.CanHandle(tt.path)
		if got != tt.want {
			t.Errorf("CanHandle(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestPythonIntrospector_ExtractTools(t *testing.T) {
	if _, err := findPython(); err != nil {
		t.Skip("python3/python not available:", err)
	}

	p := &PythonIntrospector{}
	filePath := filepath.Join(testdataDir(), "python_simple", "math_tools.py")
	tools, err := p.ExtractTools(context.Background(), filePath)
	if err != nil {
		t.Fatalf("ExtractTools() error: %v", err)
	}

	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}

	// Check first tool
	add := tools[0]
	if add.Name != "add_numbers" {
		t.Errorf("expected name 'add_numbers', got %q", add.Name)
	}
	if add.Description != "Add two numbers together and return the result. Returns: int." {
		t.Errorf("unexpected description: %q", add.Description)
	}
	if add.Language != "python" {
		t.Errorf("expected language 'python', got %q", add.Language)
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
	if aProp["type"] != "integer" {
		t.Errorf("expected 'a' type 'integer', got %v", aProp["type"])
	}

	// Check second tool
	mul := tools[1]
	if mul.Name != "multiply" {
		t.Errorf("expected name 'multiply', got %q", mul.Name)
	}
	if mul.Description != "Multiply two floating point numbers. Returns: float." {
		t.Errorf("unexpected description: %q", mul.Description)
	}
}

func TestPythonIntrospector_SkipsPrivateFunctions(t *testing.T) {
	if _, err := findPython(); err != nil {
		t.Skip("python3/python not available:", err)
	}

	p := &PythonIntrospector{}
	filePath := filepath.Join(testdataDir(), "python_simple", "string_tools.py")
	tools, err := p.ExtractTools(context.Background(), filePath)
	if err != nil {
		t.Fatalf("ExtractTools() error: %v", err)
	}

	if len(tools) != 1 {
		t.Fatalf("expected 1 tool (skipping _private_helper), got %d", len(tools))
	}

	if tools[0].Name != "reverse_string" {
		t.Errorf("expected 'reverse_string', got %q", tools[0].Name)
	}
}

func TestPythonIntrospector_InferDependencies(t *testing.T) {
	if _, err := findPython(); err != nil {
		t.Skip("python3/python not available:", err)
	}

	p := &PythonIntrospector{}
	filePath := filepath.Join(testdataDir(), "python_deps", "data_tool.py")
	deps, err := p.InferDependencies(context.Background(), filePath)
	if err != nil {
		t.Fatalf("InferDependencies() error: %v", err)
	}

	// Should find pandas and requests, filtering out os and json (stdlib)
	if len(deps) != 2 {
		t.Fatalf("expected 2 dependencies, got %d: %+v", len(deps), deps)
	}

	depNames := make(map[string]bool)
	for _, d := range deps {
		depNames[d.ImportName] = true
	}

	if !depNames["pandas"] {
		t.Error("expected dependency 'pandas'")
	}
	if !depNames["requests"] {
		t.Error("expected dependency 'requests'")
	}
	if depNames["os"] {
		t.Error("'os' should be filtered as stdlib")
	}
	if depNames["json"] {
		t.Error("'json' should be filtered as stdlib")
	}
}

func TestRegistry_ScanDirectory(t *testing.T) {
	if _, err := findPython(); err != nil {
		t.Skip("python3/python not available:", err)
	}
	// Note: Glob patterns with ** may behave differently on Windows.

	reg := NewRegistry()
	dir := filepath.Join(testdataDir(), "python_simple")
	tools, err := reg.ScanDirectory(context.Background(), dir, []string{"**/*.py"}, nil)
	if err != nil {
		t.Fatalf("ScanDirectory() error: %v", err)
	}

	// math_tools.py has 2 + string_tools.py has 1 (private skipped) = 3
	if len(tools) != 3 {
		t.Fatalf("expected 3 tools from ScanDirectory, got %d", len(tools))
	}
}

func TestPythonIntrospector_ToolDecorator(t *testing.T) {
	if _, err := findPython(); err != nil {
		t.Skip("python3/python not available:", err)
	}

	p := &PythonIntrospector{}
	filePath := filepath.Join(testdataDir(), "python_decorator", "decorated_tools.py")
	tools, err := p.ExtractTools(context.Background(), filePath)
	if err != nil {
		t.Fatalf("ExtractTools() error: %v", err)
	}

	// tool() decorator function itself is skipped (starts with decorator def inside),
	// but we expect: add, remove_record (renamed), fetch_profile, plain_function = 4 tools
	// Note: the tool() def at module level is also a function, but it won't start with _,
	// so it gets picked up too. Let's just check the decorated ones by name.
	toolMap := make(map[string]ToolMetadata)
	for _, tm := range tools {
		toolMap[tm.Name] = tm
	}

	// Check @tool(description="Add two numbers", risk="read_only")
	add, ok := toolMap["add"]
	if !ok {
		t.Fatal("expected tool 'add' to be discovered")
	}
	if add.Description != "Add two numbers Returns: int." {
		t.Errorf("add description = %q, want %q", add.Description, "Add two numbers Returns: int.")
	}
	if add.Risk != "read_only" {
		t.Errorf("add risk = %q, want %q", add.Risk, "read_only")
	}

	// Check @tool(name="remove_record", risk="destructive")
	rm, ok := toolMap["remove_record"]
	if !ok {
		t.Fatal("expected tool 'remove_record' (renamed from delete_record)")
	}
	if rm.Description != "Delete a record permanently Returns: bool." {
		t.Errorf("remove_record description = %q, want %q", rm.Description, "Delete a record permanently Returns: bool.")
	}
	if rm.Risk != "destructive" {
		t.Errorf("remove_record risk = %q, want %q", rm.Risk, "destructive")
	}

	// Check @tool(risk="network")
	fp, ok := toolMap["fetch_profile"]
	if !ok {
		t.Fatal("expected tool 'fetch_profile' to be discovered")
	}
	if fp.Risk != "network" {
		t.Errorf("fetch_profile risk = %q, want %q", fp.Risk, "network")
	}

	// Check plain function is still discovered
	pf, ok := toolMap["plain_function"]
	if !ok {
		t.Fatal("expected tool 'plain_function' to be discovered")
	}
	if pf.Description != "A plain function without decorator should still be discovered. Returns: int." {
		t.Errorf("plain_function description = %q", pf.Description)
	}
	if pf.Risk != "read_only" {
		t.Errorf("plain_function risk = %q, want %q", pf.Risk, "read_only")
	}
}

func TestRegistry_ScanDirectory_WithExclude(t *testing.T) {
	if _, err := findPython(); err != nil {
		t.Skip("python3/python not available:", err)
	}

	reg := NewRegistry()
	dir := filepath.Join(testdataDir(), "python_simple")
	tools, err := reg.ScanDirectory(context.Background(), dir, []string{"**/*.py"}, []string{"**/string_tools.py"})
	if err != nil {
		t.Fatalf("ScanDirectory() error: %v", err)
	}

	// Only math_tools.py (2 tools) since string_tools.py is excluded
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools with exclude, got %d", len(tools))
	}
}
