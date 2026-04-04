package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_DefaultsWhenNoFile(t *testing.T) {
	// Load from an empty temporary directory — no foldermcp.yaml exists.
	dir := t.TempDir()

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned unexpected error: %v", err)
	}

	// Version must be 1 (CurrentSchemaVersion).
	if cfg.Version != 1 {
		t.Errorf("expected Version=1, got %d", cfg.Version)
	}

	// Default excludes must contain key patterns.
	expectedExcludes := []string{
		"tests/**",
		"__pycache__/**",
		"node_modules/**",
		".git/**",
		".foldermcp/**",
	}
	for _, want := range expectedExcludes {
		found := false
		for _, got := range cfg.Scan.Exclude {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected default exclude %q not found in %v", want, cfg.Scan.Exclude)
		}
	}

	// Default resource excludes must contain sensitive file patterns.
	sensitiveExcludes := []string{
		"**/.env",
		"**/.env.*",
		"**/*.key",
		"**/*.pem",
		"**/*.p12",
		"**/credentials.json",
		"**/secrets.yaml",
		"**/secrets.yml",
	}
	for _, want := range sensitiveExcludes {
		found := false
		for _, got := range cfg.Scan.ResourceExclude {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected default resource exclude %q not found in %v", want, cfg.Scan.ResourceExclude)
		}
	}

	// Default includes must be present.
	if len(cfg.Scan.Include) == 0 {
		t.Error("expected default include patterns, got empty slice")
	}

	// Tools map should be non-nil but empty.
	if cfg.Tools == nil {
		t.Error("expected Tools map to be non-nil")
	}

	// MaxToolsPerContext default.
	if cfg.ToolRouting.MaxToolsPerContext != 20 {
		t.Errorf("expected MaxToolsPerContext=20, got %d", cfg.ToolRouting.MaxToolsPerContext)
	}

	// Strategy default.
	if cfg.ToolRouting.Strategy != "profile" {
		t.Errorf("expected Strategy=\"profile\", got %q", cfg.ToolRouting.Strategy)
	}
}

func TestLoadConfig_ParsesYAML(t *testing.T) {
	dir := t.TempDir()

	yamlContent := `version: 1
scan:
  include:
    - "*.go"
    - "*.md"
  exclude:
    - "vendor/**"
tools:
  shell_exec:
    state: "enabled"
    description: "Execute shell commands"
    risk: "high"
dependencies:
  python:
    - requests
  node:
    - express
tool_routing:
  max_tools_per_context: 10
  strategy: "priority"
  profiles:
    backend:
      - shell_exec
      - file_read
`

	err := os.WriteFile(filepath.Join(dir, "foldermcp.yaml"), []byte(yamlContent), 0644)
	if err != nil {
		t.Fatalf("failed to write test YAML: %v", err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned unexpected error: %v", err)
	}

	// Version.
	if cfg.Version != 1 {
		t.Errorf("expected Version=1, got %d", cfg.Version)
	}

	// Scan includes.
	if len(cfg.Scan.Include) != 2 {
		t.Fatalf("expected 2 include patterns, got %d", len(cfg.Scan.Include))
	}
	if cfg.Scan.Include[0] != "*.go" {
		t.Errorf("expected first include \"*.go\", got %q", cfg.Scan.Include[0])
	}

	// Scan excludes.
	if len(cfg.Scan.Exclude) != 1 {
		t.Fatalf("expected 1 exclude pattern, got %d", len(cfg.Scan.Exclude))
	}
	if cfg.Scan.Exclude[0] != "vendor/**" {
		t.Errorf("expected first exclude \"vendor/**\", got %q", cfg.Scan.Exclude[0])
	}

	// Tool config.
	tool, ok := cfg.Tools["shell_exec"]
	if !ok {
		t.Fatal("expected tool 'shell_exec' to exist")
	}
	if tool.State != "enabled" {
		t.Errorf("expected State=\"enabled\", got %q", tool.State)
	}
	if tool.Description != "Execute shell commands" {
		t.Errorf("expected Description=\"Execute shell commands\", got %q", tool.Description)
	}
	if tool.Risk != "high" {
		t.Errorf("expected Risk=\"high\", got %q", tool.Risk)
	}

	// Dependencies.
	if len(cfg.Dependencies.Python) != 1 || cfg.Dependencies.Python[0] != "requests" {
		t.Errorf("expected Python=[requests], got %v", cfg.Dependencies.Python)
	}
	if len(cfg.Dependencies.Node) != 1 || cfg.Dependencies.Node[0] != "express" {
		t.Errorf("expected Node=[express], got %v", cfg.Dependencies.Node)
	}

	// ToolRouting.
	if cfg.ToolRouting.MaxToolsPerContext != 10 {
		t.Errorf("expected MaxToolsPerContext=10, got %d", cfg.ToolRouting.MaxToolsPerContext)
	}
	if cfg.ToolRouting.Strategy != "priority" {
		t.Errorf("expected Strategy=\"priority\", got %q", cfg.ToolRouting.Strategy)
	}
	if profs, ok := cfg.ToolRouting.Profiles["backend"]; !ok {
		t.Error("expected profile 'backend' to exist")
	} else if len(profs) != 2 {
		t.Errorf("expected 2 tools in backend profile, got %d", len(profs))
	}
}

func TestLoadConfig_RejectsInvalidVersion(t *testing.T) {
	dir := t.TempDir()

	yamlContent := `version: 99
scan:
  include:
    - "*.go"
`

	err := os.WriteFile(filepath.Join(dir, "foldermcp.yaml"), []byte(yamlContent), 0644)
	if err != nil {
		t.Fatalf("failed to write test YAML: %v", err)
	}

	_, err = Load(dir)
	if err == nil {
		t.Fatal("expected error for unsupported version, got nil")
	}
}

func TestSaveConfig(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultConfig()
	cfg.Tools["test_tool"] = ToolConfig{
		State:       "enabled",
		Description: "A test tool",
		Risk:        "low",
	}

	err := Save(dir, cfg)
	if err != nil {
		t.Fatalf("Save returned unexpected error: %v", err)
	}

	// Verify the file was written.
	data, err := os.ReadFile(filepath.Join(dir, "foldermcp.yaml"))
	if err != nil {
		t.Fatalf("failed to read saved file: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("saved file is empty")
	}

	// Load it back and verify round-trip.
	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load after Save returned error: %v", err)
	}
	if loaded.Version != cfg.Version {
		t.Errorf("round-trip Version mismatch: %d != %d", loaded.Version, cfg.Version)
	}
	if tool, ok := loaded.Tools["test_tool"]; !ok {
		t.Error("round-trip: expected tool 'test_tool' to exist")
	} else if tool.State != "enabled" {
		t.Errorf("round-trip: expected State=\"enabled\", got %q", tool.State)
	}
}
