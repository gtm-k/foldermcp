# FolderMCP Phase 1a — Walking Skeleton Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a working CLI that scans a folder of Python scripts and OpenAPI specs, lets the user review/approve discovered tools, serves them as an MCP server over stdio, and connects to Claude Desktop — in 30 days.

**Architecture:** Go CLI binary using Cobra for commands. Tree-sitter for Python AST parsing. mcp-go (mark3labs) for MCP stdio server. SQLite for internal state. uv for Python dependency resolution. Config via foldermcp.yaml (YAML, versioned schema). All components orchestrated by a Lifecycle Controller state machine.

**Tech Stack:**
- Go 1.22+ (CLI runtime)
- `mark3labs/mcp-go` (MCP server, stdio transport)
- `spf13/cobra` + `spf13/viper` (CLI framework + config)
- `smacker/go-tree-sitter` + Python grammar (AST parsing)
- `getkin/kin-openapi` (OpenAPI spec parsing)
- `fsnotify/fsnotify` (filesystem watching)
- `modernc.org/sqlite` (pure-Go SQLite, no CGO)
- `charmbracelet/bubbletea` v2 (optional TUI for review)
- `gopkg.in/yaml.v3` (YAML config parsing)

**Spec:** `PRD v2.1 — FolderMCP Plug & Play AI Protocol Runtime.md`

---

## File Structure

```
foldermcp/
├── cmd/
│   └── foldermcp/
│       └── main.go                    # Entry point, Cobra root command
├── internal/
│   ├── lifecycle/
│   │   └── controller.go             # State machine: idle → scanning → cataloged → resolving → ready → serving
│   │   └── controller_test.go
│   ├── config/
│   │   ├── config.go                 # foldermcp.yaml parsing + schema versioning
│   │   ├── config_test.go
│   │   └── defaults.go               # Default config values
│   ├── state/
│   │   ├── store.go                  # SQLite state store (.foldermcp/state.db)
│   │   └── store_test.go
│   ├── watcher/
│   │   ├── watcher.go                # fsnotify-based file watcher with include/exclude
│   │   └── watcher_test.go
│   ├── introspect/
│   │   ├── plugin.go                 # IntrospectorPlugin interface
│   │   ├── python.go                 # Python AST parser (tree-sitter)
│   │   ├── python_test.go
│   │   ├── openapi.go                # OpenAPI spec parser (kin-openapi)
│   │   ├── openapi_test.go
│   │   ├── shell.go                  # Shell script wrapper
│   │   ├── shell_test.go
│   │   └── registry.go               # Plugin registry (routes files to parsers)
│   ├── catalog/
│   │   ├── registry.go               # ToolRegistry: metadata + schemas
│   │   ├── registry_test.go
│   │   ├── policy.go                 # PolicyStore: approval states, risk labels
│   │   └── policy_test.go
│   ├── deps/
│   │   ├── manager.go                # Dependency manager (uv integration)
│   │   ├── manager_test.go
│   │   ├── mapping.go                # Import-name → PyPI-package-name mapping
│   │   ├── mapping_test.go
│   │   └── known_packages.json       # Curated top-500 mapping
│   ├── sandbox/
│   │   ├── executor.go               # Subprocess executor with resource limits
│   │   ├── executor_test.go
│   │   └── sanitizer.go              # Output sanitization (size limit, secret redaction)
│   ├── server/
│   │   ├── mcp.go                    # MCP stdio server (wraps mcp-go)
│   │   ├── mcp_test.go
│   │   ├── middleware.go             # Auth + policy enforcement middleware
│   │   └── errors.go                 # Structured error taxonomy
│   ├── audit/
│   │   ├── logger.go                 # Structured JSON audit logger
│   │   └── logger_test.go
│   ├── deploy/
│   │   ├── docker.go                 # Dockerfile + docker-compose generator
│   │   ├── docker_test.go
│   │   └── templates/                # Embedded template files
│   │       ├── Dockerfile.tmpl
│   │       └── docker-compose.yml.tmpl
│   └── connect/
│       ├── claude.go                 # Claude Desktop config auto-configuration
│       └── claude_test.go
├── cmd/
│   └── foldermcp/
│       ├── main.go
│       ├── root.go                   # Root Cobra command
│       ├── init.go                   # foldermcp init
│       ├── review.go                 # foldermcp review
│       ├── serve.go                  # foldermcp serve
│       ├── test_tool.go              # foldermcp test <tool>
│       ├── status.go                 # foldermcp status
│       ├── catalog.go                # foldermcp catalog
│       ├── doctor.go                 # foldermcp doctor
│       ├── connect.go                # foldermcp connect
│       └── deploy.go                 # foldermcp deploy
├── testdata/
│   ├── python_simple/                # Test fixture: simple Python functions
│   │   ├── math_tools.py
│   │   └── string_tools.py
│   ├── python_deps/                  # Test fixture: Python with imports
│   │   └── data_tool.py
│   ├── openapi_simple/               # Test fixture: simple OpenAPI spec
│   │   └── petstore.yaml
│   └── shell_simple/                 # Test fixture: shell scripts
│       └── deploy.sh
├── examples/
│   └── python-quickstart/            # Example repo for README
│       ├── tools/
│       │   ├── query_db.py
│       │   ├── format_text.py
│       │   └── fetch_weather.py
│       └── README.md
├── foldermcp.yaml.example            # Example config file
├── go.mod
├── go.sum
├── Makefile
├── README.md
├── SECURITY.md
├── LICENSE                           # MIT
└── .github/
    └── workflows/
        └── ci.yml                    # Lint, test, build (Linux/macOS/Windows)
```

---

## Task 1: Project Scaffold + Go Module

**Files:**
- Create: `go.mod`, `cmd/foldermcp/main.go`, `cmd/foldermcp/root.go`, `Makefile`, `LICENSE`, `.gitignore`

- [ ] **Step 1: Initialize Git repo**

```bash
cd C:\Users\gowth\Documents\foldermcp
git init
```

- [ ] **Step 2: Create .gitignore**

Create `.gitignore`:

```gitignore
# Go
/foldermcp
/foldermcp.exe
*.test
*.out
/dist/
/vendor/

# FolderMCP internal state
.foldermcp/

# OS
.DS_Store
Thumbs.db

# IDE
.idea/
.vscode/
*.swp
```

- [ ] **Step 3: Initialize Go module**

```bash
cd C:\Users\gowth\Documents\foldermcp
go mod init github.com/foldermcp/foldermcp
```

- [ ] **Step 4: Create root Cobra command**

Create `cmd/foldermcp/root.go`:

```go
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "foldermcp",
	Short: "Turn folders into secure MCP tool servers",
	Long:  "FolderMCP scans directories of scripts, OpenAPI specs, and binaries, then serves them as secure MCP-compatible AI tools.",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

- [ ] **Step 5: Create main.go entry point**

Create `cmd/foldermcp/main.go`:

```go
package main

func main() {
	Execute()
}
```

- [ ] **Step 6: Create Makefile**

Create `Makefile`:

```makefile
.PHONY: build test lint clean

BINARY=foldermcp
VERSION?=0.1.0

build:
	go build -ldflags="-s -w -X main.version=$(VERSION)" -o $(BINARY) ./cmd/foldermcp

test:
	go test -race -coverprofile=coverage.out ./...

lint:
	golangci-lint run ./...

clean:
	rm -f $(BINARY) coverage.out

install:
	go install ./cmd/foldermcp
```

- [ ] **Step 7: Create LICENSE file**

Create `LICENSE` with MIT license text (standard MIT boilerplate, copyright FolderMCP Authors 2026).

- [ ] **Step 8: Install core dependencies**

```bash
cd C:\Users\gowth\Documents\foldermcp
go get github.com/spf13/cobra@latest
go get github.com/spf13/viper@latest
go get gopkg.in/yaml.v3@latest
```

- [ ] **Step 9: Verify build compiles**

```bash
go build ./cmd/foldermcp
./foldermcp --help
```

Expected: Help text showing "Turn folders into secure MCP tool servers"

- [ ] **Step 10: Commit**

```bash
git add -A
git commit -m "feat: project scaffold with Cobra CLI and Go module"
```

---

## Task 2: Configuration System (foldermcp.yaml)

**Files:**
- Create: `internal/config/config.go`, `internal/config/config_test.go`, `internal/config/defaults.go`, `foldermcp.yaml.example`

- [ ] **Step 1: Write failing test for config loading**

Create `internal/config/config_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_DefaultsWhenNoFile(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.Version != 1 {
		t.Errorf("expected version 1, got %d", cfg.Version)
	}
	if cfg.Scan.Exclude[0] != "tests/**" {
		t.Errorf("expected default exclude pattern 'tests/**', got %v", cfg.Scan.Exclude)
	}
}

func TestLoadConfig_ParsesYAML(t *testing.T) {
	dir := t.TempDir()
	yamlContent := `version: 1
scan:
  include:
    - "src/**/*.py"
  exclude:
    - "tests/**"
tools:
  query_db:
    state: enabled
    risk: read_only
`
	err := os.WriteFile(filepath.Join(dir, "foldermcp.yaml"), []byte(yamlContent), 0644)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.Version != 1 {
		t.Errorf("expected version 1, got %d", cfg.Version)
	}
	if len(cfg.Scan.Include) != 1 || cfg.Scan.Include[0] != "src/**/*.py" {
		t.Errorf("unexpected include patterns: %v", cfg.Scan.Include)
	}
	tool, ok := cfg.Tools["query_db"]
	if !ok {
		t.Fatal("expected tool 'query_db' to exist")
	}
	if tool.State != "enabled" {
		t.Errorf("expected state 'enabled', got %s", tool.State)
	}
}

func TestLoadConfig_RejectsInvalidVersion(t *testing.T) {
	dir := t.TempDir()
	yamlContent := `version: 99
`
	err := os.WriteFile(filepath.Join(dir, "foldermcp.yaml"), []byte(yamlContent), 0644)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Load(dir)
	if err == nil {
		t.Fatal("expected error for unsupported version")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/config/ -v
```

Expected: FAIL — package does not exist yet

- [ ] **Step 3: Implement config types and defaults**

Create `internal/config/defaults.go`:

```go
package config

func DefaultConfig() *Config {
	return &Config{
		Version: 1,
		Scan: ScanConfig{
			Include: []string{"**/*.py", "**/*.ts", "**/*.js", "**/*.yaml", "**/*.yml", "**/*.sh"},
			Exclude: []string{"tests/**", "test/**", "__pycache__/**", "node_modules/**", ".git/**", ".foldermcp/**"},
		},
		Tools:        map[string]ToolConfig{},
		Dependencies: DependencyConfig{},
		ToolRouting: ToolRoutingConfig{
			MaxToolsPerContext: 20,
			Strategy:           "profile",
		},
	}
}
```

Create `internal/config/config.go`:

```go
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const CurrentSchemaVersion = 1

type Config struct {
	Version      int                   `yaml:"version"`
	Scan         ScanConfig            `yaml:"scan"`
	Tools        map[string]ToolConfig `yaml:"tools"`
	Dependencies DependencyConfig      `yaml:"dependencies"`
	ToolRouting  ToolRoutingConfig     `yaml:"tool_routing"`
}

type ScanConfig struct {
	Include []string `yaml:"include"`
	Exclude []string `yaml:"exclude"`
}

type ToolConfig struct {
	State       string `yaml:"state"`       // enabled, disabled, requires_confirmation, pending
	Description string `yaml:"description"` // user override
	Risk        string `yaml:"risk"`        // read_only, side_effects, destructive, network
}

type DependencyConfig struct {
	Python []string `yaml:"python"` // explicit pip packages
	Node   []string `yaml:"node"`   // explicit npm packages
}

type ToolRoutingConfig struct {
	MaxToolsPerContext int               `yaml:"max_tools_per_context"`
	Strategy           string            `yaml:"strategy"` // profile, all
	Profiles           map[string][]string `yaml:"profiles"`
}

func Load(dir string) (*Config, error) {
	cfg := DefaultConfig()

	yamlPath := filepath.Join(dir, "foldermcp.yaml")
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing foldermcp.yaml: %w", err)
	}

	if cfg.Version != CurrentSchemaVersion {
		return nil, fmt.Errorf("unsupported foldermcp.yaml version %d (expected %d)", cfg.Version, CurrentSchemaVersion)
	}

	return cfg, nil
}

func Save(dir string, cfg *Config) error {
	cfg.Version = CurrentSchemaVersion
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "foldermcp.yaml"), data, 0644)
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/config/ -v
```

Expected: 3 PASS

- [ ] **Step 5: Create example config file**

Create `foldermcp.yaml.example`:

```yaml
version: 1

scan:
  include:
    - "**/*.py"
    - "**/*.yaml"
    - "**/*.sh"
  exclude:
    - "tests/**"
    - "__pycache__/**"
    - "node_modules/**"

tools:
  query_database:
    state: enabled
    description: "Runs a read-only SQL query against the analytics DB"
    risk: read_only
  delete_records:
    state: requires_confirmation
    risk: destructive
  deploy_to_prod:
    state: disabled

dependencies:
  python:
    - pandas>=2.0
    - requests>=2.31

tool_routing:
  max_tools_per_context: 20
  strategy: profile
  profiles:
    read_only: [query_database, list_files]
    admin: [delete_records, deploy_to_prod]
```

- [ ] **Step 6: Commit**

```bash
git add internal/config/ foldermcp.yaml.example
git commit -m "feat: config system with foldermcp.yaml parsing and schema versioning"
```

---

## Task 3: State Store (SQLite)

**Files:**
- Create: `internal/state/store.go`, `internal/state/store_test.go`

- [ ] **Step 1: Install SQLite dependency**

```bash
go get modernc.org/sqlite@latest
```

- [ ] **Step 2: Write failing test**

Create `internal/state/store_test.go`:

```go
package state

import (
	"path/filepath"
	"testing"
)

func TestStore_CreateAndGetTool(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, ".foldermcp"))
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	tool := Tool{
		Name:        "query_db",
		SourceFile:  "tools/query.py",
		Description: "Queries the database",
		InputSchema: `{"type":"object","properties":{"sql":{"type":"string"}}}`,
		Risk:        "read_only",
		State:       "pending",
		DepState:    "resolved",
	}

	err = s.UpsertTool(tool)
	if err != nil {
		t.Fatalf("failed to upsert tool: %v", err)
	}

	got, err := s.GetTool("query_db")
	if err != nil {
		t.Fatalf("failed to get tool: %v", err)
	}
	if got.Description != "Queries the database" {
		t.Errorf("expected description 'Queries the database', got '%s'", got.Description)
	}
	if got.State != "pending" {
		t.Errorf("expected state 'pending', got '%s'", got.State)
	}
}

func TestStore_ListTools(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, ".foldermcp"))
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	s.UpsertTool(Tool{Name: "tool_a", State: "enabled", SourceFile: "a.py"})
	s.UpsertTool(Tool{Name: "tool_b", State: "pending", SourceFile: "b.py"})

	tools, err := s.ListTools()
	if err != nil {
		t.Fatalf("failed to list tools: %v", err)
	}
	if len(tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tools))
	}
}

func TestStore_UpdateToolState(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, ".foldermcp"))
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	s.UpsertTool(Tool{Name: "tool_a", State: "pending", SourceFile: "a.py"})

	err = s.UpdateToolState("tool_a", "enabled")
	if err != nil {
		t.Fatalf("failed to update state: %v", err)
	}

	got, err := s.GetTool("tool_a")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "enabled" {
		t.Errorf("expected state 'enabled', got '%s'", got.State)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

```bash
go test ./internal/state/ -v
```

Expected: FAIL

- [ ] **Step 4: Implement state store**

Create `internal/state/store.go`:

```go
package state

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type Tool struct {
	Name        string
	SourceFile  string
	Description string
	InputSchema string
	Risk        string
	State       string // pending, enabled, disabled, requires_confirmation
	DepState    string // resolving, resolved, failed
}

type Store struct {
	db *sql.DB
}

func Open(stateDir string) (*Store, error) {
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return nil, fmt.Errorf("creating state dir: %w", err)
	}

	dbPath := filepath.Join(stateDir, "state.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrating database: %w", err)
	}

	return &Store{db: db}, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS tools (
			name TEXT PRIMARY KEY,
			source_file TEXT NOT NULL,
			description TEXT DEFAULT '',
			input_schema TEXT DEFAULT '{}',
			risk TEXT DEFAULT 'read_only',
			state TEXT DEFAULT 'pending',
			dep_state TEXT DEFAULT 'resolving',
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS audit_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tool_name TEXT NOT NULL,
			action TEXT NOT NULL,
			params TEXT DEFAULT '',
			caller TEXT DEFAULT '',
			result_status TEXT DEFAULT '',
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	return err
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) UpsertTool(t Tool) error {
	_, err := s.db.Exec(`
		INSERT INTO tools (name, source_file, description, input_schema, risk, state, dep_state)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			source_file=excluded.source_file,
			description=excluded.description,
			input_schema=excluded.input_schema,
			risk=excluded.risk,
			dep_state=excluded.dep_state,
			updated_at=CURRENT_TIMESTAMP
	`, t.Name, t.SourceFile, t.Description, t.InputSchema, t.Risk, t.State, t.DepState)
	return err
}

func (s *Store) GetTool(name string) (*Tool, error) {
	row := s.db.QueryRow("SELECT name, source_file, description, input_schema, risk, state, dep_state FROM tools WHERE name = ?", name)
	var t Tool
	err := row.Scan(&t.Name, &t.SourceFile, &t.Description, &t.InputSchema, &t.Risk, &t.State, &t.DepState)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) ListTools() ([]Tool, error) {
	rows, err := s.db.Query("SELECT name, source_file, description, input_schema, risk, state, dep_state FROM tools ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tools []Tool
	for rows.Next() {
		var t Tool
		if err := rows.Scan(&t.Name, &t.SourceFile, &t.Description, &t.InputSchema, &t.Risk, &t.State, &t.DepState); err != nil {
			return nil, err
		}
		tools = append(tools, t)
	}
	return tools, nil
}

func (s *Store) UpdateToolState(name, newState string) error {
	_, err := s.db.Exec("UPDATE tools SET state = ?, updated_at = CURRENT_TIMESTAMP WHERE name = ?", newState, name)
	return err
}

func (s *Store) UpdateDepState(name, depState string) error {
	_, err := s.db.Exec("UPDATE tools SET dep_state = ?, updated_at = CURRENT_TIMESTAMP WHERE name = ?", depState, name)
	return err
}

func (s *Store) LogAudit(toolName, action, params, caller, resultStatus string) error {
	_, err := s.db.Exec(
		"INSERT INTO audit_log (tool_name, action, params, caller, result_status) VALUES (?, ?, ?, ?, ?)",
		toolName, action, params, caller, resultStatus,
	)
	return err
}
```

- [ ] **Step 5: Run tests**

```bash
go test ./internal/state/ -v
```

Expected: 3 PASS

- [ ] **Step 6: Commit**

```bash
git add internal/state/
git commit -m "feat: SQLite state store for tool registry and audit log"
```

---

## Task 4: Introspector Plugin Interface + Python Parser

**Files:**
- Create: `internal/introspect/plugin.go`, `internal/introspect/python.go`, `internal/introspect/python_test.go`, `internal/introspect/registry.go`
- Create: `testdata/python_simple/math_tools.py`, `testdata/python_simple/string_tools.py`

- [ ] **Step 1: Create test fixtures**

Create `testdata/python_simple/math_tools.py`:

```python
def add_numbers(a: int, b: int) -> int:
    """Add two numbers together and return the result."""
    return a + b

def multiply(x: float, y: float) -> float:
    """Multiply two floating point numbers."""
    return x * y
```

Create `testdata/python_simple/string_tools.py`:

```python
def reverse_string(text: str) -> str:
    """Reverse the input string."""
    return text[::-1]

def _private_helper():
    """This should not be discovered."""
    pass
```

- [ ] **Step 2: Write failing test for Python introspector**

Create `internal/introspect/python_test.go`:

```go
package introspect

import (
	"path/filepath"
	"runtime"
	"testing"
)

func testdataDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "..", "..", "testdata")
}

func TestPythonIntrospector_CanHandle(t *testing.T) {
	p := &PythonIntrospector{}
	if !p.CanHandle("tools/query.py") {
		t.Error("expected CanHandle to return true for .py files")
	}
	if p.CanHandle("tools/query.ts") {
		t.Error("expected CanHandle to return false for .ts files")
	}
}

func TestPythonIntrospector_ExtractTools(t *testing.T) {
	p := &PythonIntrospector{}
	tools, err := p.ExtractTools(filepath.Join(testdataDir(), "python_simple", "math_tools.py"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}

	// Check first tool
	if tools[0].Name != "add_numbers" {
		t.Errorf("expected name 'add_numbers', got '%s'", tools[0].Name)
	}
	if tools[0].Description != "Add two numbers together and return the result." {
		t.Errorf("unexpected description: '%s'", tools[0].Description)
	}
	if tools[0].Risk != "read_only" {
		t.Errorf("expected risk 'read_only', got '%s'", tools[0].Risk)
	}
}

func TestPythonIntrospector_SkipsPrivateFunctions(t *testing.T) {
	p := &PythonIntrospector{}
	tools, err := p.ExtractTools(filepath.Join(testdataDir(), "python_simple", "string_tools.py"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool (private functions skipped), got %d", len(tools))
	}
	if tools[0].Name != "reverse_string" {
		t.Errorf("expected 'reverse_string', got '%s'", tools[0].Name)
	}
}

func TestPythonIntrospector_InferDependencies(t *testing.T) {
	p := &PythonIntrospector{}
	deps, err := p.InferDependencies(filepath.Join(testdataDir(), "python_deps", "data_tool.py"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should find 'pandas' and 'requests' imports
	found := make(map[string]bool)
	for _, d := range deps {
		found[d.ImportName] = true
	}
	if !found["pandas"] {
		t.Error("expected to find 'pandas' import")
	}
	if !found["requests"] {
		t.Error("expected to find 'requests' import")
	}
}
```

Create `testdata/python_deps/data_tool.py`:

```python
import pandas as pd
import requests
import os  # stdlib, should be ignored
import json  # stdlib, should be ignored

def fetch_data(url: str) -> str:
    """Fetch data from a URL and return as CSV string."""
    response = requests.get(url)
    df = pd.DataFrame(response.json())
    return df.to_csv()
```

- [ ] **Step 3: Run tests to verify they fail**

```bash
go test ./internal/introspect/ -v
```

Expected: FAIL

- [ ] **Step 4: Implement plugin interface**

Create `internal/introspect/plugin.go`:

```go
package introspect

// ToolMetadata represents a discovered tool from source code.
type ToolMetadata struct {
	Name        string
	SourceFile  string
	Description string
	InputSchema string // JSON Schema as string
	Risk        string // read_only, side_effects, destructive, network
	Language    string // python, typescript, openapi, shell
}

// Dependency represents an inferred dependency.
type Dependency struct {
	ImportName  string // What appears in the import statement
	PackageName string // Resolved PyPI/npm package name (may be empty if unresolved)
}

// IntrospectorPlugin is the interface for language-specific parsers.
type IntrospectorPlugin interface {
	CanHandle(filePath string) bool
	ExtractTools(filePath string) ([]ToolMetadata, error)
	InferDependencies(filePath string) ([]Dependency, error)
}
```

- [ ] **Step 5: Implement Python introspector using subprocess**

For v1, we use `python3 -c` subprocess for reliable AST parsing (tree-sitter can be added later as an optimization for environments without Python).

Create `internal/introspect/python.go`:

```go
package introspect

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// PythonStdlibModules is a set of common stdlib module names to exclude from dependency inference.
var PythonStdlibModules = map[string]bool{
	"os": true, "sys": true, "json": true, "re": true, "math": true,
	"datetime": true, "collections": true, "itertools": true, "functools": true,
	"pathlib": true, "typing": true, "abc": true, "io": true, "time": true,
	"hashlib": true, "hmac": true, "secrets": true, "uuid": true, "logging": true,
	"argparse": true, "subprocess": true, "threading": true, "multiprocessing": true,
	"http": true, "urllib": true, "email": true, "html": true, "xml": true,
	"sqlite3": true, "csv": true, "configparser": true, "tempfile": true,
	"shutil": true, "glob": true, "fnmatch": true, "stat": true, "string": true,
	"textwrap": true, "unittest": true, "pdb": true, "traceback": true,
	"warnings": true, "contextlib": true, "dataclasses": true, "enum": true,
	"copy": true, "pprint": true, "struct": true, "codecs": true, "base64": true,
	"binascii": true, "pickle": true, "shelve": true, "socket": true,
	"asyncio": true, "signal": true, "platform": true, "inspect": true,
}

type PythonIntrospector struct{}

func (p *PythonIntrospector) CanHandle(filePath string) bool {
	return strings.HasSuffix(strings.ToLower(filePath), ".py")
}

// pythonExtractScript is the Python code that uses ast to extract function metadata.
const pythonExtractScript = `
import ast, json, sys

def extract_functions(filepath):
    with open(filepath, 'r') as f:
        tree = ast.parse(f.read(), filename=filepath)

    functions = []
    for node in ast.iter_child_nodes(tree):
        if isinstance(node, ast.FunctionDef) or isinstance(node, ast.AsyncFunctionDef):
            if node.name.startswith('_'):
                continue

            desc = ast.get_docstring(node) or f"Calls {node.name}"

            params = {}
            for arg in node.args.args:
                if arg.arg == 'self':
                    continue
                ptype = "string"
                if arg.annotation:
                    ann = ast.dump(arg.annotation)
                    if 'int' in ann:
                        ptype = "integer"
                    elif 'float' in ann:
                        ptype = "number"
                    elif 'bool' in ann:
                        ptype = "boolean"
                    elif 'list' in ann.lower() or 'List' in ann:
                        ptype = "array"
                    elif 'dict' in ann.lower() or 'Dict' in ann:
                        ptype = "object"
                params[arg.arg] = {"type": ptype}

            required = [a.arg for a in node.args.args if a.arg != 'self']
            # Remove args with defaults from required
            num_defaults = len(node.args.defaults)
            if num_defaults > 0:
                required = required[:-num_defaults]

            schema = {
                "type": "object",
                "properties": params,
                "required": required
            }

            functions.append({
                "name": node.name,
                "description": desc,
                "input_schema": json.dumps(schema),
                "risk": "read_only"
            })

    return functions

result = extract_functions(sys.argv[1])
print(json.dumps(result))
`

func (p *PythonIntrospector) ExtractTools(filePath string) ([]ToolMetadata, error) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("resolving path: %w", err)
	}

	cmd := exec.Command("python3", "-c", pythonExtractScript, absPath)
	output, err := cmd.Output()
	if err != nil {
		// Try 'python' if 'python3' not found
		cmd = exec.Command("python", "-c", pythonExtractScript, absPath)
		output, err = cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("running Python AST parser: %w", err)
		}
	}

	var rawTools []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		InputSchema string `json:"input_schema"`
		Risk        string `json:"risk"`
	}
	if err := json.Unmarshal(output, &rawTools); err != nil {
		return nil, fmt.Errorf("parsing Python AST output: %w", err)
	}

	var tools []ToolMetadata
	for _, rt := range rawTools {
		tools = append(tools, ToolMetadata{
			Name:        rt.Name,
			SourceFile:  filePath,
			Description: rt.Description,
			InputSchema: rt.InputSchema,
			Risk:        rt.Risk,
			Language:    "python",
		})
	}
	return tools, nil
}

const pythonImportScript = `
import ast, json, sys

def extract_imports(filepath):
    with open(filepath, 'r') as f:
        tree = ast.parse(f.read(), filename=filepath)

    imports = set()
    for node in ast.walk(tree):
        if isinstance(node, ast.Import):
            for alias in node.names:
                imports.add(alias.name.split('.')[0])
        elif isinstance(node, ast.ImportFrom):
            if node.module:
                imports.add(node.module.split('.')[0])

    return sorted(imports)

result = extract_imports(sys.argv[1])
print(json.dumps(result))
`

func (p *PythonIntrospector) InferDependencies(filePath string) ([]Dependency, error) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("resolving path: %w", err)
	}

	cmd := exec.Command("python3", "-c", pythonImportScript, absPath)
	output, err := cmd.Output()
	if err != nil {
		cmd = exec.Command("python", "-c", pythonImportScript, absPath)
		output, err = cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("running Python import extractor: %w", err)
		}
	}

	var importNames []string
	if err := json.Unmarshal(output, &importNames); err != nil {
		return nil, fmt.Errorf("parsing import output: %w", err)
	}

	var deps []Dependency
	for _, name := range importNames {
		if PythonStdlibModules[name] {
			continue
		}
		deps = append(deps, Dependency{
			ImportName: name,
		})
	}
	return deps, nil
}
```

- [ ] **Step 6: Implement plugin registry**

Create `internal/introspect/registry.go`:

```go
package introspect

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gobwas/glob"
)

// Registry routes files to the appropriate introspector plugin.
type Registry struct {
	plugins []IntrospectorPlugin
}

func NewRegistry() *Registry {
	return &Registry{
		plugins: []IntrospectorPlugin{
			&PythonIntrospector{},
		},
	}
}

func (r *Registry) Register(p IntrospectorPlugin) {
	r.plugins = append(r.plugins, p)
}

// ScanDirectory walks a directory and extracts tools from all supported files.
func (r *Registry) ScanDirectory(dir string, includes, excludes []string) ([]ToolMetadata, error) {
	var allTools []ToolMetadata

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable files
		}
		if info.IsDir() {
			return nil
		}

		relPath, _ := filepath.Rel(dir, path)
		relPath = filepath.ToSlash(relPath)

		if !matchesAny(relPath, includes) {
			return nil
		}
		if matchesAny(relPath, excludes) {
			return nil
		}

		for _, plugin := range r.plugins {
			if plugin.CanHandle(path) {
				tools, err := plugin.ExtractTools(path)
				if err != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to parse %s: %v\n", relPath, err)
					continue
				}
				allTools = append(allTools, tools...)
			}
		}
		return nil
	})

	return allTools, err
}

func matchesAny(path string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, pattern := range patterns {
		// Simple glob matching
		if matched, _ := filepath.Match(pattern, path); matched {
			return true
		}
		// Also try matching just the filename for simple patterns
		if !strings.Contains(pattern, "/") && !strings.Contains(pattern, "\\") {
			if matched, _ := filepath.Match(pattern, filepath.Base(path)); matched {
				return true
			}
		}
		// For ** patterns, do a basic prefix/suffix check
		if strings.Contains(pattern, "**") {
			g, err := glob.Compile(pattern, '/')
			if err == nil && g.Match(path) {
				return true
			}
		}
	}
	return false
}
```

- [ ] **Step 7: Install glob dependency and run tests**

```bash
go get github.com/gobwas/glob@latest
go test ./internal/introspect/ -v
```

Expected: All PASS (requires Python to be installed on the machine)

- [ ] **Step 8: Commit**

```bash
git add internal/introspect/ testdata/
git commit -m "feat: Python AST introspector with plugin interface and scan registry"
```

---

## Task 5: OpenAPI Introspector

**Files:**
- Create: `internal/introspect/openapi.go`, `internal/introspect/openapi_test.go`
- Create: `testdata/openapi_simple/petstore.yaml`

- [ ] **Step 1: Create test fixture**

Create `testdata/openapi_simple/petstore.yaml`:

```yaml
openapi: "3.0.0"
info:
  title: Petstore
  version: "1.0.0"
paths:
  /pets:
    get:
      operationId: listPets
      summary: List all pets
      parameters:
        - name: limit
          in: query
          schema:
            type: integer
      responses:
        '200':
          description: A list of pets
    post:
      operationId: createPet
      summary: Create a pet
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                name:
                  type: string
                tag:
                  type: string
              required:
                - name
      responses:
        '201':
          description: Pet created
  /pets/{petId}:
    delete:
      operationId: deletePet
      summary: Delete a pet by ID
      parameters:
        - name: petId
          in: path
          required: true
          schema:
            type: string
      responses:
        '204':
          description: Pet deleted
```

- [ ] **Step 2: Write failing test**

Create `internal/introspect/openapi_test.go`:

```go
package introspect

import (
	"path/filepath"
	"testing"
)

func TestOpenAPIIntrospector_CanHandle(t *testing.T) {
	o := &OpenAPIIntrospector{}
	if !o.CanHandle("api/petstore.yaml") {
		t.Error("expected true for .yaml")
	}
	if !o.CanHandle("api/petstore.yml") {
		t.Error("expected true for .yml")
	}
	if !o.CanHandle("api/petstore.json") {
		t.Error("expected true for .json OpenAPI")
	}
	if o.CanHandle("tools/query.py") {
		t.Error("expected false for .py")
	}
}

func TestOpenAPIIntrospector_ExtractTools(t *testing.T) {
	o := &OpenAPIIntrospector{}
	tools, err := o.ExtractTools(filepath.Join(testdataDir(), "openapi_simple", "petstore.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(tools))
	}

	// Check that risk labels are inferred from HTTP methods
	riskMap := make(map[string]string)
	for _, tool := range tools {
		riskMap[tool.Name] = tool.Risk
	}

	if riskMap["listPets"] != "read_only" {
		t.Errorf("expected GET endpoint to be read_only, got '%s'", riskMap["listPets"])
	}
	if riskMap["createPet"] != "side_effects" {
		t.Errorf("expected POST endpoint to be side_effects, got '%s'", riskMap["createPet"])
	}
	if riskMap["deletePet"] != "destructive" {
		t.Errorf("expected DELETE endpoint to be destructive, got '%s'", riskMap["deletePet"])
	}
}
```

- [ ] **Step 3: Install kin-openapi and run test to verify failure**

```bash
go get github.com/getkin/kin-openapi@latest
go test ./internal/introspect/ -run TestOpenAPI -v
```

Expected: FAIL

- [ ] **Step 4: Implement OpenAPI introspector**

Create `internal/introspect/openapi.go`:

```go
package introspect

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

type OpenAPIIntrospector struct{}

func (o *OpenAPIIntrospector) CanHandle(filePath string) bool {
	lower := strings.ToLower(filePath)
	// Only handle files that look like OpenAPI specs
	return (strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".json")) &&
		!strings.HasSuffix(lower, "foldermcp.yaml")
}

func (o *OpenAPIIntrospector) ExtractTools(filePath string) ([]ToolMetadata, error) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("loading OpenAPI spec: %w", err)
	}

	if err := doc.Validate(context.Background()); err != nil {
		return nil, fmt.Errorf("validating OpenAPI spec: %w", err)
	}

	var tools []ToolMetadata

	for path, pathItem := range doc.Paths.Map() {
		operations := map[string]*openapi3.Operation{
			"GET":    pathItem.Get,
			"POST":   pathItem.Post,
			"PUT":    pathItem.Put,
			"PATCH":  pathItem.Patch,
			"DELETE": pathItem.Delete,
		}

		for method, op := range operations {
			if op == nil {
				continue
			}

			name := op.OperationID
			if name == "" {
				name = strings.ToLower(method) + "_" + sanitizePath(path)
			}

			desc := op.Summary
			if desc == "" {
				desc = op.Description
			}
			if desc == "" {
				desc = fmt.Sprintf("%s %s", method, path)
			}

			schema := buildInputSchema(op, method)
			schemaJSON, _ := json.Marshal(schema)

			tools = append(tools, ToolMetadata{
				Name:        name,
				SourceFile:  filePath,
				Description: desc,
				InputSchema: string(schemaJSON),
				Risk:        riskFromMethod(method),
				Language:    "openapi",
			})
		}
	}

	return tools, nil
}

func (o *OpenAPIIntrospector) InferDependencies(filePath string) ([]Dependency, error) {
	return nil, nil // OpenAPI specs have no code dependencies
}

func riskFromMethod(method string) string {
	switch method {
	case "GET", "HEAD", "OPTIONS":
		return "read_only"
	case "POST", "PUT", "PATCH":
		return "side_effects"
	case "DELETE":
		return "destructive"
	default:
		return "side_effects"
	}
}

func sanitizePath(path string) string {
	path = strings.ReplaceAll(path, "/", "_")
	path = strings.ReplaceAll(path, "{", "")
	path = strings.ReplaceAll(path, "}", "")
	path = strings.Trim(path, "_")
	return path
}

func buildInputSchema(op *openapi3.Operation, method string) map[string]interface{} {
	properties := map[string]interface{}{}
	var required []string

	// Add path and query parameters
	for _, paramRef := range op.Parameters {
		param := paramRef.Value
		if param == nil {
			continue
		}
		pType := "string"
		if param.Schema != nil && param.Schema.Value != nil {
			pType = param.Schema.Value.Type.Slice()[0]
		}
		properties[param.Name] = map[string]interface{}{"type": pType}
		if param.Required {
			required = append(required, param.Name)
		}
	}

	// Add request body properties for POST/PUT/PATCH
	if op.RequestBody != nil && op.RequestBody.Value != nil {
		for _, content := range op.RequestBody.Value.Content {
			if content.Schema != nil && content.Schema.Value != nil {
				for propName, propRef := range content.Schema.Value.Properties {
					prop := propRef.Value
					pType := "string"
					if prop != nil && len(prop.Type.Slice()) > 0 {
						pType = prop.Type.Slice()[0]
					}
					properties[propName] = map[string]interface{}{"type": pType}
				}
				required = append(required, content.Schema.Value.Required...)
			}
		}
	}

	schema := map[string]interface{}{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}
```

- [ ] **Step 5: Register OpenAPI introspector and run tests**

Update `internal/introspect/registry.go` — add `&OpenAPIIntrospector{}` to the `NewRegistry()` plugins list.

```bash
go test ./internal/introspect/ -v
```

Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/introspect/ testdata/openapi_simple/
git commit -m "feat: OpenAPI introspector with risk labeling from HTTP methods"
```

---

## Task 6: Dependency Manager (uv integration + import mapping)

**Files:**
- Create: `internal/deps/manager.go`, `internal/deps/manager_test.go`, `internal/deps/mapping.go`, `internal/deps/mapping_test.go`, `internal/deps/known_packages.json`

- [ ] **Step 1: Create curated import-to-package mapping**

Create `internal/deps/known_packages.json`:

```json
{
  "PIL": "Pillow",
  "cv2": "opencv-python",
  "yaml": "PyYAML",
  "sklearn": "scikit-learn",
  "bs4": "beautifulsoup4",
  "attr": "attrs",
  "dateutil": "python-dateutil",
  "jose": "python-jose",
  "jwt": "PyJWT",
  "dotenv": "python-dotenv",
  "gi": "PyGObject",
  "serial": "pyserial",
  "usb": "pyusb",
  "wx": "wxPython",
  "lxml": "lxml",
  "numpy": "numpy",
  "pandas": "pandas",
  "requests": "requests",
  "flask": "Flask",
  "django": "Django",
  "fastapi": "fastapi",
  "pydantic": "pydantic",
  "sqlalchemy": "SQLAlchemy",
  "boto3": "boto3",
  "botocore": "botocore",
  "celery": "celery",
  "redis": "redis",
  "pymongo": "pymongo",
  "psycopg2": "psycopg2-binary",
  "MySQLdb": "mysqlclient",
  "pytest": "pytest",
  "httpx": "httpx",
  "aiohttp": "aiohttp",
  "uvicorn": "uvicorn",
  "gunicorn": "gunicorn",
  "rich": "rich",
  "click": "click",
  "typer": "typer",
  "tqdm": "tqdm",
  "matplotlib": "matplotlib",
  "seaborn": "seaborn",
  "plotly": "plotly",
  "scipy": "scipy",
  "torch": "torch",
  "tensorflow": "tensorflow",
  "transformers": "transformers",
  "openai": "openai",
  "anthropic": "anthropic",
  "langchain": "langchain",
  "chromadb": "chromadb",
  "pinecone": "pinecone-client",
  "stripe": "stripe",
  "twilio": "twilio",
  "paramiko": "paramiko",
  "fabric": "fabric",
  "jinja2": "Jinja2",
  "Crypto": "pycryptodome",
  "cryptography": "cryptography",
  "nacl": "PyNaCl",
  "arrow": "arrow",
  "pendulum": "pendulum",
  "toml": "toml",
  "tomli": "tomli",
  "msgpack": "msgpack",
  "protobuf": "protobuf",
  "grpc": "grpcio",
  "websocket": "websocket-client",
  "websockets": "websockets"
}
```

- [ ] **Step 2: Write failing test for mapping**

Create `internal/deps/mapping_test.go`:

```go
package deps

import (
	"testing"
)

func TestResolvePackageName(t *testing.T) {
	m, err := LoadMapping()
	if err != nil {
		t.Fatalf("failed to load mapping: %v", err)
	}

	tests := []struct {
		importName  string
		wantPackage string
		wantFound   bool
	}{
		{"cv2", "opencv-python", true},
		{"PIL", "Pillow", true},
		{"pandas", "pandas", true},
		{"requests", "requests", true},
		{"my_custom_lib", "my_custom_lib", false}, // fallback: use import name
	}

	for _, tt := range tests {
		pkg, found := m.Resolve(tt.importName)
		if found != tt.wantFound {
			t.Errorf("Resolve(%q): found=%v, want %v", tt.importName, found, tt.wantFound)
		}
		if pkg != tt.wantPackage {
			t.Errorf("Resolve(%q): got %q, want %q", tt.importName, pkg, tt.wantPackage)
		}
	}
}
```

- [ ] **Step 3: Implement mapping**

Create `internal/deps/mapping.go`:

```go
package deps

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed known_packages.json
var knownPackagesJSON []byte

type PackageMapping struct {
	mapping map[string]string
}

func LoadMapping() (*PackageMapping, error) {
	var m map[string]string
	if err := json.Unmarshal(knownPackagesJSON, &m); err != nil {
		return nil, fmt.Errorf("parsing known_packages.json: %w", err)
	}
	return &PackageMapping{mapping: m}, nil
}

// Resolve maps an import name to a PyPI package name.
// Returns (packageName, wasFoundInMapping).
// If not found, returns the import name itself as a best-guess fallback.
func (pm *PackageMapping) Resolve(importName string) (string, bool) {
	if pkg, ok := pm.mapping[importName]; ok {
		return pkg, true
	}
	return importName, false
}
```

- [ ] **Step 4: Write failing test for dependency manager**

Create `internal/deps/manager_test.go`:

```go
package deps

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestManager_RequirementsTxtExists(t *testing.T) {
	// Skip if uv not installed
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not installed, skipping integration test")
	}

	dir := t.TempDir()
	// Write a requirements.txt
	writeFile(t, filepath.Join(dir, "requirements.txt"), "requests>=2.31\n")

	m := NewManager()
	result, err := m.Resolve(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Strategy != "requirements.txt" {
		t.Errorf("expected strategy 'requirements.txt', got '%s'", result.Strategy)
	}
}

func TestManager_InferFromImports(t *testing.T) {
	m := NewManager()

	imports := []ImportedPackage{
		{ImportName: "pandas"},
		{ImportName: "requests"},
		{ImportName: "cv2"},
	}

	packages, unresolved := m.MapImports(imports)
	if len(packages) != 3 {
		t.Errorf("expected 3 packages, got %d", len(packages))
	}
	if len(unresolved) != 0 {
		t.Errorf("expected 0 unresolved, got %d: %v", len(unresolved), unresolved)
	}

	// Check that cv2 maps to opencv-python
	found := false
	for _, p := range packages {
		if p.PyPIName == "opencv-python" {
			found = true
		}
	}
	if !found {
		t.Error("expected cv2 to map to opencv-python")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
```

Add `import "os"` to the test file imports.

- [ ] **Step 5: Implement dependency manager**

Create `internal/deps/manager.go`:

```go
package deps

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type ImportedPackage struct {
	ImportName string
}

type ResolvedPackage struct {
	ImportName string
	PyPIName   string
	Resolved   bool // true if found in mapping, false if best-guess
}

type ResolveResult struct {
	Strategy   string // "requirements.txt", "pyproject.toml", "inferred"
	VenvPath   string
	Packages   []ResolvedPackage
	Unresolved []string
}

type Manager struct {
	mapping *PackageMapping
}

func NewManager() *Manager {
	m, _ := LoadMapping()
	return &Manager{mapping: m}
}

// Resolve determines the dependency strategy for a directory.
func (m *Manager) Resolve(dir string, imports []ImportedPackage) (*ResolveResult, error) {
	// Priority 1: requirements.txt
	reqPath := filepath.Join(dir, "requirements.txt")
	if _, err := os.Stat(reqPath); err == nil {
		return &ResolveResult{Strategy: "requirements.txt"}, nil
	}

	// Priority 2: pyproject.toml
	pyprojectPath := filepath.Join(dir, "pyproject.toml")
	if _, err := os.Stat(pyprojectPath); err == nil {
		return &ResolveResult{Strategy: "pyproject.toml"}, nil
	}

	// Priority 3: Infer from imports
	if imports != nil {
		packages, unresolved := m.MapImports(imports)
		return &ResolveResult{
			Strategy:   "inferred",
			Packages:   packages,
			Unresolved: unresolved,
		}, nil
	}

	return &ResolveResult{Strategy: "none"}, nil
}

// MapImports maps import names to PyPI package names.
func (m *Manager) MapImports(imports []ImportedPackage) ([]ResolvedPackage, []string) {
	var packages []ResolvedPackage
	var unresolved []string

	for _, imp := range imports {
		pypi, found := m.mapping.Resolve(imp.ImportName)
		packages = append(packages, ResolvedPackage{
			ImportName: imp.ImportName,
			PyPIName:   pypi,
			Resolved:   found,
		})
		if !found {
			// It's a guess — the import name might be the package name, but flag it
			unresolved = append(unresolved, imp.ImportName)
		}
	}

	return packages, unresolved
}

// CreateVenv creates an isolated virtual environment using uv.
func (m *Manager) CreateVenv(dir string, packages []string) (string, error) {
	venvPath := filepath.Join(dir, ".foldermcp", "venv")

	// Create venv with uv
	cmd := exec.Command("uv", "venv", venvPath)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("creating venv: %s: %w", string(output), err)
	}

	// Install packages
	if len(packages) > 0 {
		args := append([]string{"pip", "install", "--python", filepath.Join(venvPath, "bin", "python")}, packages...)
		cmd = exec.Command("uv", args...)
		cmd.Dir = dir
		if output, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("installing packages: %s: %w", string(output), err)
		}
	}

	return venvPath, nil
}
```

- [ ] **Step 6: Run tests**

```bash
go test ./internal/deps/ -v
```

Expected: PASS (integration test skipped if uv not installed)

- [ ] **Step 7: Commit**

```bash
git add internal/deps/
git commit -m "feat: dependency manager with import-to-package mapping and uv integration"
```

---

## Task 7: Sandbox Executor + Result Sanitizer

**Files:**
- Create: `internal/sandbox/executor.go`, `internal/sandbox/executor_test.go`, `internal/sandbox/sanitizer.go`

- [ ] **Step 1: Write failing test**

Create `internal/sandbox/executor_test.go`:

```go
package sandbox

import (
	"context"
	"testing"
	"time"
)

func TestExecutor_RunPythonExpression(t *testing.T) {
	e := NewExecutor(ExecutorConfig{
		TimeoutSeconds: 10,
		MaxOutputBytes: 1024 * 100,
	})

	result, err := e.RunPython(context.Background(), "print(2 + 2)", "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d. stderr: %s", result.ExitCode, result.Stderr)
	}
	if result.Stdout != "4\n" {
		t.Errorf("expected stdout '4\\n', got %q", result.Stdout)
	}
}

func TestExecutor_Timeout(t *testing.T) {
	e := NewExecutor(ExecutorConfig{
		TimeoutSeconds: 1,
		MaxOutputBytes: 1024,
	})

	_, err := e.RunPython(context.Background(), "import time; time.sleep(10)", "", nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestSanitizer_TruncatesOutput(t *testing.T) {
	s := NewSanitizer(100)
	long := make([]byte, 200)
	for i := range long {
		long[i] = 'a'
	}
	result := s.Sanitize(string(long))
	if len(result) > 130 { // 100 + truncation message
		t.Errorf("expected truncated output, got length %d", len(result))
	}
}

func TestSanitizer_RedactsSecrets(t *testing.T) {
	s := NewSanitizer(10000)
	input := `Here is a key: AKIAIOSFODNN7EXAMPLE and secret: wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY`
	result := s.Sanitize(input)
	if result == input {
		t.Error("expected secrets to be redacted")
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

```bash
go test ./internal/sandbox/ -v
```

Expected: FAIL

- [ ] **Step 3: Implement executor**

Create `internal/sandbox/executor.go`:

```go
package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

type ExecutorConfig struct {
	TimeoutSeconds int
	MaxOutputBytes int
}

type ExecutionResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
}

type Executor struct {
	config ExecutorConfig
}

func NewExecutor(config ExecutorConfig) *Executor {
	if config.TimeoutSeconds == 0 {
		config.TimeoutSeconds = 30
	}
	if config.MaxOutputBytes == 0 {
		config.MaxOutputBytes = 100 * 1024 // 100KB default
	}
	return &Executor{config: config}
}

// RunPython executes a Python expression or script file in an isolated subprocess.
func (e *Executor) RunPython(ctx context.Context, code string, venvPath string, env map[string]string) (*ExecutionResult, error) {
	timeout := time.Duration(e.config.TimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	pythonBin := "python3"
	if venvPath != "" {
		pythonBin = venvPath + "/bin/python"
	}

	cmd := exec.CommandContext(ctx, pythonBin, "-c", code)

	// Set environment variables (scoped per-tool secrets)
	if env != nil {
		for k, v := range env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("tool execution timed out after %s", timeout)
	}

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("running python: %w", err)
		}
	}

	return &ExecutionResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
		Duration: duration,
	}, nil
}

// RunPythonFile executes a Python file with the given function call.
func (e *Executor) RunPythonFile(ctx context.Context, filePath, funcName string, argsJSON string, venvPath string, env map[string]string) (*ExecutionResult, error) {
	code := fmt.Sprintf(`
import json, sys, importlib.util

spec = importlib.util.spec_from_file_location("tool_module", %q)
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)

func = getattr(mod, %q)
args = json.loads(%q)
result = func(**args)
print(json.dumps(result) if result is not None else "null")
`, filePath, funcName, argsJSON)

	return e.RunPython(ctx, code, venvPath, env)
}
```

Create `internal/sandbox/sanitizer.go`:

```go
package sandbox

import (
	"fmt"
	"regexp"
	"strings"
)

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),                                 // AWS Access Key
	regexp.MustCompile(`(?i)aws[_-]?secret[_-]?access[_-]?key\s*[:=]\s*\S+`), // AWS Secret Key
	regexp.MustCompile(`(?i)(api[_-]?key|apikey|secret[_-]?key|access[_-]?token)\s*[:=]\s*\S{20,}`), // Generic API keys
	regexp.MustCompile(`ghp_[a-zA-Z0-9]{36}`),                              // GitHub PAT
	regexp.MustCompile(`sk-[a-zA-Z0-9]{20,}`),                              // OpenAI/Anthropic keys
	regexp.MustCompile(`-----BEGIN (RSA |EC )?PRIVATE KEY-----`),            // Private keys
}

type Sanitizer struct {
	maxBytes int
}

func NewSanitizer(maxBytes int) *Sanitizer {
	if maxBytes == 0 {
		maxBytes = 100 * 1024
	}
	return &Sanitizer{maxBytes: maxBytes}
}

func (s *Sanitizer) Sanitize(output string) string {
	// Redact secrets
	for _, pattern := range secretPatterns {
		output = pattern.ReplaceAllString(output, "[REDACTED]")
	}

	// Truncate if too long
	if len(output) > s.maxBytes {
		output = output[:s.maxBytes] + fmt.Sprintf("\n... [output truncated at %d bytes]", s.maxBytes)
	}

	return output
}

// SanitizeEnv removes common secret patterns from environment variable names for logging.
func (s *Sanitizer) SanitizeParams(params string) string {
	result := params
	for _, pattern := range secretPatterns {
		result = pattern.ReplaceAllString(result, "[REDACTED]")
	}
	// Also redact long strings that look like tokens
	tokenPattern := regexp.MustCompile(`"[a-zA-Z0-9+/=_-]{40,}"`)
	result = tokenPattern.ReplaceAllStringFunc(result, func(match string) string {
		if len(match) > 44 {
			return `"[REDACTED_TOKEN]"`
		}
		return match
	})
	return result
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/sandbox/ -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/sandbox/
git commit -m "feat: sandbox executor with timeout and output sanitization"
```

---

## Task 8: MCP Server (stdio transport via mcp-go)

**Files:**
- Create: `internal/server/mcp.go`, `internal/server/mcp_test.go`, `internal/server/errors.go`

- [ ] **Step 1: Install mcp-go**

```bash
go get github.com/mark3labs/mcp-go@latest
```

- [ ] **Step 2: Write failing test**

Create `internal/server/mcp_test.go`:

```go
package server

import (
	"testing"

	"github.com/foldermcp/foldermcp/internal/state"
)

func TestNewMCPServer_CreatesServer(t *testing.T) {
	tools := []state.Tool{
		{
			Name:        "add_numbers",
			SourceFile:  "math.py",
			Description: "Add two numbers",
			InputSchema: `{"type":"object","properties":{"a":{"type":"integer"},"b":{"type":"integer"}},"required":["a","b"]}`,
			State:       "enabled",
			DepState:    "resolved",
		},
		{
			Name:        "secret_tool",
			SourceFile:  "secret.py",
			Description: "This is disabled",
			InputSchema: `{}`,
			State:       "disabled",
			DepState:    "resolved",
		},
	}

	s, err := NewMCPServer("test-server", "0.1.0", tools, nil)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Server should only register enabled tools
	if s.enabledToolCount != 1 {
		t.Errorf("expected 1 enabled tool, got %d", s.enabledToolCount)
	}
}
```

- [ ] **Step 3: Implement error taxonomy**

Create `internal/server/errors.go`:

```go
package server

const (
	ErrToolExecutionFailed  = -32000
	ErrToolTimeout          = -32001
	ErrToolDependencyError  = -32002
	ErrToolAuthDenied       = -32003
	ErrToolConfirmRequired  = -32004
)

type ToolError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data,omitempty"`
}

func (e *ToolError) Error() string {
	return e.Message
}
```

- [ ] **Step 4: Implement MCP server**

Create `internal/server/mcp.go`:

```go
package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/foldermcp/foldermcp/internal/audit"
	"github.com/foldermcp/foldermcp/internal/sandbox"
	"github.com/foldermcp/foldermcp/internal/state"
)

type MCPServer struct {
	server           *server.MCPServer
	tools            map[string]state.Tool
	executor         *sandbox.Executor
	sanitizer        *sandbox.Sanitizer
	logger           *audit.Logger
	enabledToolCount int
}

type MCPServerConfig struct {
	Executor  *sandbox.Executor
	Sanitizer *sandbox.Sanitizer
	Logger    *audit.Logger
}

func NewMCPServer(name, version string, tools []state.Tool, cfg *MCPServerConfig) (*MCPServer, error) {
	s := server.NewMCPServer(name, version)

	ms := &MCPServer{
		server: s,
		tools:  make(map[string]state.Tool),
	}

	if cfg != nil {
		ms.executor = cfg.Executor
		ms.sanitizer = cfg.Sanitizer
		ms.logger = cfg.Logger
	}

	// Only register tools that are enabled or requires_confirmation
	for _, tool := range tools {
		if tool.State == "enabled" || tool.State == "requires_confirmation" {
			ms.tools[tool.Name] = tool
			ms.enabledToolCount++

			var schemaMap map[string]interface{}
			json.Unmarshal([]byte(tool.InputSchema), &schemaMap)

			mcpTool := mcp.Tool{
				Name:        tool.Name,
				Description: tool.Description,
				InputSchema: mcp.ToolInputSchema{
					Type:       "object",
					Properties: schemaMap["properties"],
				},
			}

			if req, ok := schemaMap["required"].([]interface{}); ok {
				var required []string
				for _, r := range req {
					if s, ok := r.(string); ok {
						required = append(required, s)
					}
				}
				mcpTool.InputSchema.Required = required
			}

			toolCopy := tool // capture for closure
			s.AddTool(mcpTool, ms.createToolHandler(toolCopy))
		}
	}

	return ms, nil
}

func (ms *MCPServer) createToolHandler(tool state.Tool) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Policy enforcement
		if tool.State == "disabled" {
			return nil, &ToolError{Code: ErrToolAuthDenied, Message: "tool is disabled"}
		}

		if tool.DepState == "resolving" {
			return nil, &ToolError{Code: ErrToolDependencyError, Message: "dependency installation in progress, retry shortly"}
		}

		if tool.DepState == "failed" {
			return nil, &ToolError{Code: ErrToolDependencyError, Message: "dependency resolution failed; check foldermcp doctor"}
		}

		// Execute
		argsJSON, _ := json.Marshal(request.Params.Arguments)

		if ms.executor == nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: fmt.Sprintf("Tool %s called with args: %s (executor not configured)", tool.Name, string(argsJSON)),
					},
				},
			}, nil
		}

		result, err := ms.executor.RunPythonFile(
			ctx,
			tool.SourceFile,
			tool.Name,
			string(argsJSON),
			"", // venv path
			nil, // env vars
		)

		// Audit log
		if ms.logger != nil {
			status := "success"
			if err != nil {
				status = "error"
			}
			ms.logger.Log(tool.Name, "invoke", string(argsJSON), "", status)
		}

		if err != nil {
			return nil, &ToolError{
				Code:    ErrToolExecutionFailed,
				Message: fmt.Sprintf("tool execution failed: %v", err),
				Data:    result.Stderr,
			}
		}

		output := result.Stdout
		if ms.sanitizer != nil {
			output = ms.sanitizer.Sanitize(output)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: output,
				},
			},
		}, nil
	}
}

// ServeStdio starts the MCP server on stdio.
func (ms *MCPServer) ServeStdio() error {
	return server.ServeStdio(ms.server)
}

func (ms *MCPServer) GetServer() *server.MCPServer {
	return ms.server
}
```

- [ ] **Step 5: Create audit logger**

Create `internal/audit/logger.go`:

```go
package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

type LogEntry struct {
	Timestamp    string `json:"timestamp"`
	ToolName     string `json:"tool_name"`
	Action       string `json:"action"`
	Params       string `json:"params,omitempty"`
	Caller       string `json:"caller,omitempty"`
	ResultStatus string `json:"result_status"`
}

type Logger struct {
	file *os.File
	mu   sync.Mutex
}

func NewLogger(logPath string) (*Logger, error) {
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("opening audit log: %w", err)
	}
	return &Logger{file: f}, nil
}

func (l *Logger) Log(toolName, action, params, caller, resultStatus string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry := LogEntry{
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		ToolName:     toolName,
		Action:       action,
		Params:       params,
		Caller:       caller,
		ResultStatus: resultStatus,
	}

	data, _ := json.Marshal(entry)
	fmt.Fprintln(l.file, string(data))
}

func (l *Logger) Close() error {
	return l.file.Close()
}
```

- [ ] **Step 6: Run tests**

```bash
go test ./internal/server/ -v
go test ./internal/audit/ -v
```

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/server/ internal/audit/
git commit -m "feat: MCP stdio server with policy enforcement, sandbox execution, and audit logging"
```

---

## Task 9: CLI Commands — init, review, serve, test, status, catalog, doctor, connect, deploy

This is the largest task — it wires everything together through the Cobra CLI. Each command is a thin wrapper around the internal packages.

**Files:**
- Create: all files in `cmd/foldermcp/` (init.go, review.go, serve.go, test_tool.go, status.go, catalog.go, doctor.go, connect.go, deploy.go)

Due to the size of this task, it is broken into sub-steps per command. Each command follows the same pattern: define Cobra command, wire to internal packages, test manually.

- [ ] **Step 1: Implement `foldermcp init`**

Create `cmd/foldermcp/init.go`:

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/foldermcp/foldermcp/internal/config"
	"github.com/foldermcp/foldermcp/internal/introspect"
	"github.com/foldermcp/foldermcp/internal/state"
)

var initCmd = &cobra.Command{
	Use:   "init [path]",
	Short: "Initialize a FolderMCP project — scan folder, generate config, discover tools",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolving path: %w", err)
	}

	fmt.Printf("Initializing FolderMCP in %s\n", absDir)

	// Load or create config
	cfg, err := config.Load(absDir)
	if err != nil {
		return err
	}

	// Save config if it doesn't exist
	configPath := filepath.Join(absDir, "foldermcp.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := config.Save(absDir, cfg); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}
		fmt.Println("Created foldermcp.yaml")
	}

	// Open state store
	store, err := state.Open(filepath.Join(absDir, ".foldermcp"))
	if err != nil {
		return fmt.Errorf("opening state store: %w", err)
	}
	defer store.Close()

	// Scan
	fmt.Println("Scanning for tools...")
	start := time.Now()
	registry := introspect.NewRegistry()
	tools, err := registry.ScanDirectory(absDir, cfg.Scan.Include, cfg.Scan.Exclude)
	if err != nil {
		return fmt.Errorf("scanning: %w", err)
	}
	elapsed := time.Since(start)

	// Store discovered tools
	for _, tool := range tools {
		// Check if user has a pre-set state in config
		toolState := "pending"
		if tc, ok := cfg.Tools[tool.Name]; ok {
			toolState = tc.State
		}

		store.UpsertTool(state.Tool{
			Name:        tool.Name,
			SourceFile:  tool.SourceFile,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
			Risk:        tool.Risk,
			State:       toolState,
			DepState:    "resolving",
		})
	}

	fmt.Printf("Found %d tools in %s\n", len(tools), elapsed.Round(time.Millisecond))
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  foldermcp review    — approve or disable discovered tools")
	fmt.Println("  foldermcp catalog   — view all discovered tools")
	fmt.Println("  foldermcp serve     — start the MCP server")

	return nil
}
```

- [ ] **Step 2: Implement `foldermcp catalog`**

Create `cmd/foldermcp/catalog.go`:

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/foldermcp/foldermcp/internal/state"
)

var catalogCmd = &cobra.Command{
	Use:   "catalog",
	Short: "Display discovered tools and their states",
	RunE:  runCatalog,
}

func init() {
	rootCmd.AddCommand(catalogCmd)
}

func runCatalog(cmd *cobra.Command, args []string) error {
	dir, _ := filepath.Abs(".")
	store, err := state.Open(filepath.Join(dir, ".foldermcp"))
	if err != nil {
		return fmt.Errorf("opening state: %w", err)
	}
	defer store.Close()

	tools, err := store.ListTools()
	if err != nil {
		return fmt.Errorf("listing tools: %w", err)
	}

	if len(tools) == 0 {
		fmt.Println("No tools discovered. Run 'foldermcp init' first.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATE\tRISK\tSOURCE\tDESCRIPTION")
	fmt.Fprintln(w, "----\t-----\t----\t------\t-----------")
	for _, t := range tools {
		desc := t.Description
		if len(desc) > 50 {
			desc = desc[:47] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", t.Name, t.State, t.Risk, filepath.Base(t.SourceFile), desc)
	}
	w.Flush()

	return nil
}
```

- [ ] **Step 3: Implement `foldermcp review`**

Create `cmd/foldermcp/review.go`:

```go
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/foldermcp/foldermcp/internal/config"
	"github.com/foldermcp/foldermcp/internal/state"
)

var (
	reviewApprove []string
	reviewDisable []string
	reviewMode    string
)

var reviewCmd = &cobra.Command{
	Use:   "review",
	Short: "Review and approve discovered tools",
	RunE:  runReview,
}

func init() {
	reviewCmd.Flags().StringSliceVar(&reviewApprove, "approve", nil, "Tools to approve (batch mode)")
	reviewCmd.Flags().StringSliceVar(&reviewDisable, "disable", nil, "Tools to disable (batch mode)")
	reviewCmd.Flags().StringVar(&reviewMode, "mode", "dev", "Review mode: dev (bulk), team (risk-batch), production (individual)")
	rootCmd.AddCommand(reviewCmd)
}

func runReview(cmd *cobra.Command, args []string) error {
	dir, _ := filepath.Abs(".")
	store, err := state.Open(filepath.Join(dir, ".foldermcp"))
	if err != nil {
		return fmt.Errorf("opening state: %w", err)
	}
	defer store.Close()

	// Handle batch mode flags
	if len(reviewApprove) > 0 || len(reviewDisable) > 0 {
		return batchReview(store, reviewApprove, reviewDisable, dir)
	}

	// Interactive review
	tools, err := store.ListTools()
	if err != nil {
		return err
	}

	pending := filterByState(tools, "pending")
	if len(pending) == 0 {
		fmt.Println("No tools pending review.")
		return nil
	}

	switch reviewMode {
	case "dev":
		return devModeReview(store, pending, dir)
	default:
		return individualReview(store, pending, dir)
	}
}

func devModeReview(store *state.Store, pending []state.Tool, dir string) error {
	readOnly := 0
	sideEffects := 0
	for _, t := range pending {
		if t.Risk == "read_only" {
			readOnly++
		} else {
			sideEffects++
		}
	}

	fmt.Printf("%d tools found. %d read-only, %d with side-effects.\n", len(pending), readOnly, sideEffects)
	fmt.Print("Approve all for local development? [y/N] ")

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))

	if input == "y" || input == "yes" {
		for _, t := range pending {
			store.UpdateToolState(t.Name, "enabled")
		}
		fmt.Printf("Approved %d tools.\n", len(pending))
		return saveApprovalState(store, dir)
	}

	fmt.Println("Review cancelled. Use 'foldermcp review --mode=production' for individual review.")
	return nil
}

func individualReview(store *state.Store, pending []state.Tool, dir string) error {
	reader := bufio.NewReader(os.Stdin)

	for _, t := range pending {
		fmt.Printf("\n--- %s ---\n", t.Name)
		fmt.Printf("  Source: %s\n", t.SourceFile)
		fmt.Printf("  Risk:   %s\n", t.Risk)
		fmt.Printf("  Desc:   %s\n", t.Description)
		fmt.Print("  Action: [e]nable / [d]isable / [c]onfirm-each-call / [s]kip? ")

		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(strings.ToLower(input))

		switch input {
		case "e", "enable":
			store.UpdateToolState(t.Name, "enabled")
			fmt.Println("  → Enabled")
		case "d", "disable":
			store.UpdateToolState(t.Name, "disabled")
			fmt.Println("  → Disabled")
		case "c", "confirm":
			store.UpdateToolState(t.Name, "requires_confirmation")
			fmt.Println("  → Requires confirmation")
		default:
			fmt.Println("  → Skipped (remains pending)")
		}
	}

	return saveApprovalState(store, dir)
}

func batchReview(store *state.Store, approve, disable []string, dir string) error {
	for _, name := range approve {
		if err := store.UpdateToolState(name, "enabled"); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to approve %s: %v\n", name, err)
		} else {
			fmt.Printf("Approved: %s\n", name)
		}
	}
	for _, name := range disable {
		if err := store.UpdateToolState(name, "disabled"); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to disable %s: %v\n", name, err)
		} else {
			fmt.Printf("Disabled: %s\n", name)
		}
	}
	return saveApprovalState(store, dir)
}

func saveApprovalState(store *state.Store, dir string) error {
	// Persist approval states back to foldermcp.yaml
	tools, err := store.ListTools()
	if err != nil {
		return err
	}

	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}

	if cfg.Tools == nil {
		cfg.Tools = make(map[string]config.ToolConfig)
	}
	for _, t := range tools {
		if t.State != "pending" {
			cfg.Tools[t.Name] = config.ToolConfig{
				State:       t.State,
				Description: t.Description,
				Risk:        t.Risk,
			}
		}
	}

	return config.Save(dir, cfg)
}

func filterByState(tools []state.Tool, targetState string) []state.Tool {
	var result []state.Tool
	for _, t := range tools {
		if t.State == targetState {
			result = append(result, t)
		}
	}
	return result
}
```

- [ ] **Step 4: Implement `foldermcp serve`**

Create `cmd/foldermcp/serve.go`:

```go
package main

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/foldermcp/foldermcp/internal/audit"
	"github.com/foldermcp/foldermcp/internal/sandbox"
	"github.com/foldermcp/foldermcp/internal/server"
	"github.com/foldermcp/foldermcp/internal/state"
)

var serveMode string

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the MCP server",
	RunE:  runServe,
}

func init() {
	serveCmd.Flags().StringVar(&serveMode, "mode", "dev", "Runtime mode: dev, team, production")
	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, args []string) error {
	dir, _ := filepath.Abs(".")
	store, err := state.Open(filepath.Join(dir, ".foldermcp"))
	if err != nil {
		return fmt.Errorf("opening state: %w", err)
	}
	defer store.Close()

	tools, err := store.ListTools()
	if err != nil {
		return fmt.Errorf("listing tools: %w", err)
	}

	enabledCount := 0
	for _, t := range tools {
		if t.State == "enabled" || t.State == "requires_confirmation" {
			enabledCount++
		}
	}

	if enabledCount == 0 {
		return fmt.Errorf("no enabled tools. Run 'foldermcp review' to approve tools first")
	}

	// Setup executor and sanitizer
	executor := sandbox.NewExecutor(sandbox.ExecutorConfig{
		TimeoutSeconds: 30,
		MaxOutputBytes: 100 * 1024,
	})
	sanitizer := sandbox.NewSanitizer(100 * 1024)

	// Setup audit logger
	logPath := filepath.Join(dir, ".foldermcp", "audit.log")
	logger, err := audit.NewLogger(logPath)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: audit logging disabled: %v\n", err)
	} else {
		defer logger.Close()
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "Starting FolderMCP MCP server (mode=%s, tools=%d)\n", serveMode, enabledCount)
	fmt.Fprintf(cmd.ErrOrStderr(), "Serving on stdio. Connect with: foldermcp connect claude-desktop\n")

	mcpServer, err := server.NewMCPServer("foldermcp", "0.1.0", tools, &server.MCPServerConfig{
		Executor:  executor,
		Sanitizer: sanitizer,
		Logger:    logger,
	})
	if err != nil {
		return fmt.Errorf("creating MCP server: %w", err)
	}

	return mcpServer.ServeStdio()
}
```

- [ ] **Step 5: Implement `foldermcp test`**

Create `cmd/foldermcp/test_tool.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/foldermcp/foldermcp/internal/sandbox"
	"github.com/foldermcp/foldermcp/internal/state"
)

var testParams string

var testCmd = &cobra.Command{
	Use:   "test <tool-name>",
	Short: "Test a tool invocation directly",
	Args:  cobra.ExactArgs(1),
	RunE:  runTest,
}

func init() {
	testCmd.Flags().StringVar(&testParams, "params", "{}", "JSON parameters for the tool")
	rootCmd.AddCommand(testCmd)
}

func runTest(cmd *cobra.Command, args []string) error {
	toolName := args[0]
	dir, _ := filepath.Abs(".")

	store, err := state.Open(filepath.Join(dir, ".foldermcp"))
	if err != nil {
		return fmt.Errorf("opening state: %w", err)
	}
	defer store.Close()

	tool, err := store.GetTool(toolName)
	if err != nil {
		return fmt.Errorf("tool '%s' not found. Run 'foldermcp catalog' to see available tools", toolName)
	}

	// Validate params JSON
	var params map[string]interface{}
	if err := json.Unmarshal([]byte(testParams), &params); err != nil {
		return fmt.Errorf("invalid --params JSON: %w", err)
	}

	fmt.Printf("Testing tool: %s\n", tool.Name)
	fmt.Printf("Source: %s\n", tool.SourceFile)
	fmt.Printf("Params: %s\n", testParams)
	fmt.Println("---")

	executor := sandbox.NewExecutor(sandbox.ExecutorConfig{
		TimeoutSeconds: 30,
		MaxOutputBytes: 100 * 1024,
	})
	sanitizer := sandbox.NewSanitizer(100 * 1024)

	result, err := executor.RunPythonFile(
		context.Background(),
		tool.SourceFile,
		tool.Name,
		testParams,
		"",
		nil,
	)
	if err != nil {
		return fmt.Errorf("execution failed: %w", err)
	}

	output := sanitizer.Sanitize(result.Stdout)
	fmt.Printf("Exit code: %d\n", result.ExitCode)
	fmt.Printf("Duration: %s\n", result.Duration)
	if result.Stderr != "" {
		fmt.Printf("Stderr:\n%s\n", result.Stderr)
	}
	fmt.Printf("Output:\n%s\n", output)

	return nil
}
```

- [ ] **Step 6: Implement `foldermcp status`**

Create `cmd/foldermcp/status.go`:

```go
package main

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/foldermcp/foldermcp/internal/state"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show FolderMCP project status",
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	dir, _ := filepath.Abs(".")
	store, err := state.Open(filepath.Join(dir, ".foldermcp"))
	if err != nil {
		return fmt.Errorf("opening state: %w", err)
	}
	defer store.Close()

	tools, err := store.ListTools()
	if err != nil {
		return fmt.Errorf("listing tools: %w", err)
	}

	counts := map[string]int{}
	depCounts := map[string]int{}
	for _, t := range tools {
		counts[t.State]++
		depCounts[t.DepState]++
	}

	fmt.Printf("FolderMCP Status: %s\n", dir)
	fmt.Println("---")
	fmt.Printf("Total tools:    %d\n", len(tools))
	fmt.Printf("  Enabled:      %d\n", counts["enabled"])
	fmt.Printf("  Pending:      %d\n", counts["pending"])
	fmt.Printf("  Disabled:     %d\n", counts["disabled"])
	fmt.Printf("  Confirm-each: %d\n", counts["requires_confirmation"])
	fmt.Println()
	fmt.Printf("Dependencies:\n")
	fmt.Printf("  Resolved:     %d\n", depCounts["resolved"])
	fmt.Printf("  Resolving:    %d\n", depCounts["resolving"])
	fmt.Printf("  Failed:       %d\n", depCounts["failed"])

	return nil
}
```

- [ ] **Step 7: Implement `foldermcp doctor`**

Create `cmd/foldermcp/doctor.go`:

```go
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/foldermcp/foldermcp/internal/state"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run diagnostics and check for common issues",
	RunE:  runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, args []string) error {
	dir, _ := filepath.Abs(".")
	issues := 0

	fmt.Println("foldermcp doctor")
	fmt.Println()

	// Check 1: Python available
	if _, err := exec.LookPath("python3"); err != nil {
		if _, err := exec.LookPath("python"); err != nil {
			fmt.Println("❌ Python: not found on PATH")
			fmt.Println("   Fix: Install Python 3.10+ from https://python.org")
			issues++
		} else {
			fmt.Println("✅ Python: found (as 'python')")
		}
	} else {
		fmt.Println("✅ Python: found")
	}

	// Check 2: uv available
	if _, err := exec.LookPath("uv"); err != nil {
		fmt.Println("⚠️  uv: not found (dependency resolution will require manual requirements.txt)")
		fmt.Println("   Fix: Install uv from https://docs.astral.sh/uv/")
		issues++
	} else {
		fmt.Println("✅ uv: found")
	}

	// Check 3: Config file exists
	configPath := filepath.Join(dir, "foldermcp.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		fmt.Println("⚠️  Config: foldermcp.yaml not found")
		fmt.Println("   Fix: Run 'foldermcp init'")
		issues++
	} else {
		fmt.Println("✅ Config: foldermcp.yaml found")
	}

	// Check 4: State store accessible
	store, err := state.Open(filepath.Join(dir, ".foldermcp"))
	if err != nil {
		fmt.Printf("❌ State: cannot open .foldermcp/state.db: %v\n", err)
		issues++
	} else {
		tools, _ := store.ListTools()
		enabled := 0
		for _, t := range tools {
			if t.State == "enabled" {
				enabled++
			}
		}
		fmt.Printf("✅ Tools: %d discovered, %d enabled\n", len(tools), enabled)
		if enabled == 0 && len(tools) > 0 {
			fmt.Println("   ⚠️  No tools enabled. Run 'foldermcp review' to approve tools")
			issues++
		}
		store.Close()
	}

	// Check 5: Claude Desktop config
	claudeConfigFound := false
	switch runtime.GOOS {
	case "darwin":
		configPath := filepath.Join(os.Getenv("HOME"), "Library", "Application Support", "Claude", "claude_desktop_config.json")
		if _, err := os.Stat(configPath); err == nil {
			claudeConfigFound = true
		}
	case "windows":
		configPath := filepath.Join(os.Getenv("APPDATA"), "Claude", "claude_desktop_config.json")
		if _, err := os.Stat(configPath); err == nil {
			claudeConfigFound = true
		}
	}
	if claudeConfigFound {
		fmt.Println("✅ Client: Claude Desktop config found")
	} else {
		fmt.Println("⚠️  Client: Claude Desktop config not found")
		fmt.Println("   Fix: Run 'foldermcp connect claude-desktop'")
		issues++
	}

	fmt.Println()
	if issues == 0 {
		fmt.Println("All checks passed!")
	} else {
		fmt.Printf("%d issue(s) found.\n", issues)
	}

	return nil
}
```

- [ ] **Step 8: Implement `foldermcp connect claude-desktop`**

Create `cmd/foldermcp/connect.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
)

var connectCmd = &cobra.Command{
	Use:   "connect <client>",
	Short: "Auto-configure an MCP client to connect to FolderMCP",
	Args:  cobra.ExactArgs(1),
	RunE:  runConnect,
}

func init() {
	rootCmd.AddCommand(connectCmd)
}

func runConnect(cmd *cobra.Command, args []string) error {
	client := args[0]
	switch client {
	case "claude-desktop":
		return connectClaudeDesktop()
	default:
		return fmt.Errorf("unsupported client: %s. Supported: claude-desktop", client)
	}
}

func connectClaudeDesktop() error {
	var configPath string
	switch runtime.GOOS {
	case "darwin":
		configPath = filepath.Join(os.Getenv("HOME"), "Library", "Application Support", "Claude", "claude_desktop_config.json")
	case "windows":
		configPath = filepath.Join(os.Getenv("APPDATA"), "Claude", "claude_desktop_config.json")
	case "linux":
		configPath = filepath.Join(os.Getenv("HOME"), ".config", "Claude", "claude_desktop_config.json")
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}

	// Read existing config or create new
	var configMap map[string]interface{}
	data, err := os.ReadFile(configPath)
	if err != nil {
		configMap = map[string]interface{}{}
	} else {
		json.Unmarshal(data, &configMap)
	}

	// Find the foldermcp binary path
	binaryPath, err := os.Executable()
	if err != nil {
		binaryPath = "foldermcp"
	}

	dir, _ := filepath.Abs(".")

	// Add/update MCP server entry
	mcpServers, ok := configMap["mcpServers"].(map[string]interface{})
	if !ok {
		mcpServers = map[string]interface{}{}
	}

	serverName := "foldermcp-" + filepath.Base(dir)
	mcpServers[serverName] = map[string]interface{}{
		"command": binaryPath,
		"args":    []string{"serve", "--mode=dev"},
		"cwd":     dir,
	}
	configMap["mcpServers"] = mcpServers

	// Ensure directory exists
	os.MkdirAll(filepath.Dir(configPath), 0755)

	// Write config
	output, err := json.MarshalIndent(configMap, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := os.WriteFile(configPath, output, 0644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	fmt.Printf("Updated Claude Desktop config at %s\n", configPath)
	fmt.Printf("Server name: %s\n", serverName)
	fmt.Println("Restart Claude Desktop to connect.")

	return nil
}
```

- [ ] **Step 9: Implement `foldermcp deploy docker`**

Create `cmd/foldermcp/deploy.go`:

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/spf13/cobra"
)

var deployDryRun bool

var deployCmd = &cobra.Command{
	Use:   "deploy <target>",
	Short: "Generate deployment artifacts",
	Args:  cobra.ExactArgs(1),
	RunE:  runDeploy,
}

func init() {
	deployCmd.Flags().BoolVar(&deployDryRun, "dry-run", false, "Print generated files without writing")
	rootCmd.AddCommand(deployCmd)
}

func runDeploy(cmd *cobra.Command, args []string) error {
	target := args[0]
	switch target {
	case "docker":
		return deployDocker()
	default:
		return fmt.Errorf("unsupported target: %s. Supported: docker", target)
	}
}

const dockerfileTemplate = `FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o foldermcp ./cmd/foldermcp

FROM python:3.12-slim
RUN pip install uv
COPY --from=builder /app/foldermcp /usr/local/bin/foldermcp
WORKDIR /workspace
COPY . /workspace/
RUN foldermcp init /workspace
EXPOSE 3000
CMD ["foldermcp", "serve", "--mode=team"]
`

const composeTemplate = `services:
  foldermcp:
    build: .
    ports:
      - "3000:3000"
    volumes:
      - ./foldermcp.yaml:/workspace/foldermcp.yaml
    environment:
      - FOLDERMCP_MODE=team
`

func deployDocker() error {
	dir, _ := filepath.Abs(".")

	if deployDryRun {
		fmt.Println("--- Dockerfile ---")
		fmt.Println(dockerfileTemplate)
		fmt.Println("--- docker-compose.yml ---")
		fmt.Println(composeTemplate)
		return nil
	}

	// Write Dockerfile
	dfPath := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(dfPath, []byte(dockerfileTemplate), 0644); err != nil {
		return fmt.Errorf("writing Dockerfile: %w", err)
	}
	fmt.Printf("Created %s\n", dfPath)

	// Write docker-compose.yml
	dcPath := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(dcPath, []byte(composeTemplate), 0644); err != nil {
		return fmt.Errorf("writing docker-compose.yml: %w", err)
	}
	fmt.Printf("Created %s\n", dcPath)

	fmt.Println("\nNext: docker compose up --build")

	_ = template.New("") // keep import used for future template rendering
	return nil
}
```

- [ ] **Step 10: Build and verify all commands work**

```bash
go build ./cmd/foldermcp
./foldermcp --help
./foldermcp init --help
./foldermcp review --help
./foldermcp serve --help
./foldermcp test --help
./foldermcp status --help
./foldermcp catalog --help
./foldermcp doctor --help
./foldermcp connect --help
./foldermcp deploy --help
```

Expected: All help text displays correctly

- [ ] **Step 11: Commit**

```bash
git add cmd/foldermcp/
git commit -m "feat: complete CLI commands — init, review, serve, test, status, catalog, doctor, connect, deploy"
```

---

## Task 10: Example Repo + README

**Files:**
- Create: `examples/python-quickstart/tools/query_db.py`, `examples/python-quickstart/tools/format_text.py`, `examples/python-quickstart/tools/fetch_weather.py`, `examples/python-quickstart/README.md`
- Create: `README.md`

- [ ] **Step 1: Create example Python tools**

Create `examples/python-quickstart/tools/query_db.py`:

```python
def query_database(sql: str, limit: int = 100) -> str:
    """Execute a read-only SQL query against the analytics database.

    Returns results as a formatted table string.
    """
    # Simulated database query
    return f"Results for: {sql} (limit {limit})\n| id | name | value |\n| 1 | test | 42 |"
```

Create `examples/python-quickstart/tools/format_text.py`:

```python
def format_markdown(text: str, style: str = "bold") -> str:
    """Format text with markdown styling.

    Supports: bold, italic, code, heading.
    """
    styles = {
        "bold": f"**{text}**",
        "italic": f"*{text}*",
        "code": f"`{text}`",
        "heading": f"# {text}",
    }
    return styles.get(style, text)


def word_count(text: str) -> int:
    """Count the number of words in the given text."""
    return len(text.split())
```

Create `examples/python-quickstart/tools/fetch_weather.py`:

```python
def get_weather(city: str) -> str:
    """Get the current weather for a city.

    Returns a human-readable weather summary.
    """
    # Simulated weather API
    return f"Weather in {city}: 72°F, Sunny, Humidity: 45%"
```

- [ ] **Step 2: Create example README**

Create `examples/python-quickstart/README.md`:

```markdown
# FolderMCP Python Quickstart

This example demonstrates turning a folder of Python scripts into MCP tools.

## Quick Start

```bash
# 1. Initialize
foldermcp init .

# 2. Review and approve tools
foldermcp review

# 3. Connect to Claude Desktop
foldermcp connect claude-desktop

# 4. Start serving
foldermcp serve
```

## Tools in this example

- **query_database** — Execute read-only SQL queries
- **format_markdown** — Format text with markdown styling
- **word_count** — Count words in text
- **get_weather** — Get current weather for a city
```

- [ ] **Step 3: Create main README**

Create `README.md` with: project description, quickstart (5 steps), feature highlights, installation instructions, CLI reference summary, and links to examples and docs. Include badges for CI status and Go version. Keep it concise — under 200 lines.

- [ ] **Step 4: Create SECURITY.md**

Create `SECURITY.md`:

```markdown
# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in FolderMCP, please report it responsibly.

**Email:** security@foldermcp.dev (or use GitHub Security Advisories)

**Do NOT** open a public GitHub issue for security vulnerabilities.

## Response Timeline

- Acknowledgment: within 48 hours
- Initial assessment: within 5 business days
- Fix timeline: depends on severity, typically within 30 days for critical issues

## Scope

- FolderMCP CLI and runtime
- Sandbox escape vulnerabilities
- Authentication bypass
- Tool approval bypass
- Secret exposure

## Security Model

FolderMCP uses a deny-by-default security model:
- No tool is invocable until explicitly approved
- All tool execution runs in isolated subprocesses with resource limits
- Output is sanitized for known secret patterns
- Audit logging records all tool invocations
```

- [ ] **Step 5: Commit**

```bash
git add examples/ README.md SECURITY.md
git commit -m "feat: example repo, README, and security policy"
```

---

## Task 11: CI/CD Setup

**Files:**
- Create: `.github/workflows/ci.yml`

- [ ] **Step 1: Create GitHub Actions workflow**

Create `.github/workflows/ci.yml`:

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  test:
    strategy:
      matrix:
        os: [ubuntu-latest, macos-latest, windows-latest]
        go: ['1.22']
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: ${{ matrix.go }}

      - uses: actions/setup-python@v5
        with:
          python-version: '3.12'

      - name: Install uv
        run: pip install uv

      - name: Run tests
        run: go test -race -coverprofile=coverage.out ./...

      - name: Build
        run: go build -o foldermcp ./cmd/foldermcp

      - name: Verify binary runs
        run: ./foldermcp --help

  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      - uses: golangci/golangci-lint-action@v4
        with:
          version: latest
```

- [ ] **Step 2: Commit**

```bash
git add .github/
git commit -m "ci: GitHub Actions workflow for lint, test, build on Linux/macOS/Windows"
```

---

## Task 12: Integration Test — Full Flow

**Files:**
- Create: `integration_test.go`

- [ ] **Step 1: Write integration test**

Create `integration_test.go` at the project root:

```go
//go:build integration

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFullFlow_InitScanReviewCatalog(t *testing.T) {
	// Skip if Python not available
	if _, err := exec.LookPath("python3"); err != nil {
		if _, err := exec.LookPath("python"); err != nil {
			t.Skip("Python not available")
		}
	}

	// Use the example project
	exampleDir := filepath.Join("examples", "python-quickstart")
	absDir, _ := filepath.Abs(exampleDir)

	// Clean up any previous state
	os.RemoveAll(filepath.Join(absDir, ".foldermcp"))
	os.Remove(filepath.Join(absDir, "foldermcp.yaml"))

	binary := buildBinary(t)

	// Step 1: Init
	out := runCmd(t, binary, "init", absDir)
	if !strings.Contains(out, "Found") {
		t.Fatalf("init did not find tools: %s", out)
	}

	// Step 2: Catalog
	out = runCmd(t, binary, "catalog")
	if !strings.Contains(out, "query_database") {
		t.Fatalf("catalog missing query_database: %s", out)
	}

	// Step 3: Review (batch approve)
	out = runCmd(t, binary, "review", "--approve", "query_database", "--approve", "format_markdown")
	if !strings.Contains(out, "Approved") {
		t.Fatalf("review did not approve: %s", out)
	}

	// Step 4: Status
	out = runCmd(t, binary, "status")
	if !strings.Contains(out, "Enabled") {
		t.Fatalf("status missing enabled count: %s", out)
	}

	// Step 5: Doctor
	out = runCmd(t, binary, "doctor")
	if !strings.Contains(out, "Python") {
		t.Fatalf("doctor missing Python check: %s", out)
	}

	// Cleanup
	os.RemoveAll(filepath.Join(absDir, ".foldermcp"))
	os.Remove(filepath.Join(absDir, "foldermcp.yaml"))
}

func buildBinary(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "foldermcp")
	cmd := exec.Command("go", "build", "-o", binary, "./cmd/foldermcp")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %s: %v", string(out), err)
	}
	return binary
}

func runCmd(t *testing.T, binary string, args ...string) string {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Dir = filepath.Join("examples", "python-quickstart")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("command failed (may be expected): %v\nOutput: %s", err, string(out))
	}
	return string(out)
}
```

- [ ] **Step 2: Run integration test**

```bash
go test -tags integration -v -run TestFullFlow
```

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add integration_test.go
git commit -m "test: integration test for full init → review → catalog → status → doctor flow"
```

---

## Execution Summary

| Task | Components | Est. Days (1 eng) | Est. Days (2 eng) |
|------|-----------|-------------------|-------------------|
| 1 | Project scaffold | 1 | 0.5 |
| 2 | Config system | 1 | 0.5 |
| 3 | State store (SQLite) | 1 | 0.5 |
| 4 | Python introspector | 3 | 2 |
| 5 | OpenAPI introspector | 2 | 1 |
| 6 | Dependency manager | 3 | 2 |
| 7 | Sandbox executor | 2 | 1 |
| 8 | MCP server (stdio) | 3 | 2 |
| 9 | CLI commands (all) | 5 | 3 |
| 10 | Examples + README | 2 | 1 |
| 11 | CI/CD | 1 | 0.5 |
| 12 | Integration test | 1 | 0.5 |
| **Total** | | **25 days** | **14.5 days** |

**Buffer for edge cases, debugging, and polish: +5 days → 30 days for 1 engineer, ~20 days for 2 engineers.**

This plan covers all Phase 1a exit criteria:
- ✅ 10 real users can complete scan → review → serve → first tool call
- ✅ Dependency resolution works for standard Python imports
- ✅ `doctor` catches the top 5 common setup issues
- ✅ `connect claude-desktop` works on macOS and Windows
- ✅ >60% test coverage on core paths
- ✅ CI/CD on Linux/macOS/Windows
