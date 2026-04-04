package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gtm-k/foldermcp/internal/config"
	"github.com/gtm-k/foldermcp/internal/introspect"
	"github.com/gtm-k/foldermcp/internal/state"
	"github.com/gtm-k/foldermcp/internal/workspace"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init [path]",
	Short: "Initialize a directory as a FolderMCP workspace",
	Long: `Scans the target directory for Python, TypeScript/JavaScript, OpenAPI specs,
shell scripts, and documents (PDF, images, CSV, Markdown). Creates a foldermcp.yaml
config (if missing) and populates the state store with discovered tools and resources.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runInit,
}

var initTemplate string

func init() {
	initCmd.Flags().StringVar(&initTemplate, "template", "", "Scaffold an example project (python, openapi, shell)")
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	// Handle --template scaffolding before the scan.
	if initTemplate != "" {
		if err := scaffoldTemplate(absDir, initTemplate); err != nil {
			return err
		}
		// Fall through to the normal scan logic so users don't need to
		// run "init" twice after scaffolding template files.
	}

	// Validate directory exists.
	info, err := os.Stat(absDir)
	if err != nil {
		return fmt.Errorf("directory does not exist: %s", absDir)
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory: %s", absDir)
	}

	// Open workspace (split storage: local state vs project dir).
	ws, err := workspace.Open(absDir)
	if err != nil {
		return fmt.Errorf("open workspace: %w", err)
	}

	// Load or create config.
	cfg, err := config.Load(ws.ProjectDir)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Save config if it doesn't exist yet.
	if _, err := os.Stat(ws.ConfigPath()); os.IsNotExist(err) {
		if err := config.Save(ws.ProjectDir, cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		fmt.Fprintln(os.Stderr, "Created foldermcp.yaml")
	}

	// Open state store (SQLite always on local disk).
	store, err := state.Open(ws.LocalDir)
	if err != nil {
		return fmt.Errorf("open state store: %w", err)
	}
	defer func() { _ = store.Close() }()

	if ws.IsNetworkFS {
		fmt.Fprintf(os.Stderr, "Network filesystem detected. State stored locally at %s\n", ws.LocalDir)
	}

	// Scan directory for tools.
	start := time.Now()
	registry := introspect.NewRegistry()
	tools, err := registry.ScanDirectory(context.Background(), absDir, cfg.Scan.Include, cfg.Scan.Exclude)
	if err != nil {
		return fmt.Errorf("scan directory: %w", err)
	}

	// Build the batch of tools to upsert in a single transaction.
	stateTools := make([]state.Tool, 0, len(tools))
	for _, tm := range tools {
		// Check if the tool has an override in config.
		toolState := "pending"
		risk := tm.Risk
		if tc, ok := cfg.Tools[tm.Name]; ok {
			if tc.State != "" {
				toolState = tc.State
			}
			if tc.Risk != "" {
				risk = tc.Risk
			}
		}

		schemaJSON := tm.InputSchema
		if schemaJSON == "" {
			schemaJSON = `{"type":"object","properties":{}}`
		}
		// Validate schema is valid JSON.
		if !json.Valid([]byte(schemaJSON)) {
			schemaJSON = `{"type":"object","properties":{}}`
		}

		stateTools = append(stateTools, state.Tool{
			Name:        tm.Name,
			SourceFile:  tm.SourceFile,
			Description: tm.Description,
			InputSchema: schemaJSON,
			Risk:        risk,
			State:       toolState,
		})
	}
	if err := store.UpsertToolsBatch(stateTools); err != nil {
		return fmt.Errorf("batch upsert tools: %w", err)
	}

	// Discover resources.
	resourceIntrospector := &introspect.ResourceIntrospector{}
	resources, err := resourceIntrospector.Discover(absDir, cfg.Scan.ResourceInclude, cfg.Scan.ResourceExclude)
	if err != nil {
		return fmt.Errorf("discover resources: %w", err)
	}

	// Upsert each discovered resource into state store.
	for _, rm := range resources {
		r := state.Resource{
			Name:         rm.Name,
			FilePath:     rm.FilePath,
			MimeType:     rm.MimeType,
			SizeBytes:    rm.SizeBytes,
			ResourceType: rm.Type,
		}
		if err := store.UpsertResource(r); err != nil {
			return fmt.Errorf("upsert resource %q: %w", rm.Name, err)
		}
	}

	elapsed := time.Since(start)
	fmt.Fprintf(os.Stderr, "Found %d tools and %d resources in %dms\n", len(tools), len(resources), elapsed.Milliseconds())
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Next steps:")
	fmt.Fprintln(os.Stderr, "  foldermcp review    — approve or disable discovered tools and resources")
	fmt.Fprintln(os.Stderr, "  foldermcp catalog   — list all tools and resources")
	fmt.Fprintln(os.Stderr, "  foldermcp serve     — start the MCP server")

	return nil
}

// scaffoldTemplate creates example files for the given template type.
func scaffoldTemplate(dir, template string) error {
	switch template {
	case "python":
		return scaffoldPython(dir)
	case "openapi":
		return scaffoldOpenAPI(dir)
	case "shell":
		return scaffoldShell(dir)
	default:
		return fmt.Errorf("unknown template %q; supported: python, openapi, shell", template)
	}
}

func scaffoldPython(dir string) error {
	toolsDir := filepath.Join(dir, "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		return fmt.Errorf("create tools dir: %w", err)
	}

	calcCode := `"""Example calculator tool for FolderMCP.

foldermcp:tool calculate
foldermcp:description Perform basic arithmetic operations
foldermcp:risk read_only
foldermcp:param operation string The operation to perform (add, subtract, multiply, divide)
foldermcp:param a number First operand
foldermcp:param b number Second operand
"""


def calculate(operation: str, a: float, b: float) -> float:
    """Perform basic arithmetic operations."""
    ops = {
        "add": lambda x, y: x + y,
        "subtract": lambda x, y: x - y,
        "multiply": lambda x, y: x * y,
        "divide": lambda x, y: x / y if y != 0 else float("inf"),
    }
    if operation not in ops:
        raise ValueError(f"Unknown operation: {operation}. Use: add, subtract, multiply, divide")
    return ops[operation](a, b)
`

	textCode := `"""Example text processing tool for FolderMCP.

foldermcp:tool word_count
foldermcp:description Count words, characters, and lines in text
foldermcp:risk read_only
foldermcp:param text string The text to analyze
"""


def word_count(text: str) -> dict:
    """Count words, characters, and lines in text."""
    return {
        "words": len(text.split()),
        "characters": len(text),
        "lines": len(text.splitlines()),
    }
`

	if err := os.WriteFile(filepath.Join(toolsDir, "example_calculator.py"), []byte(calcCode), 0o644); err != nil {
		return fmt.Errorf("write calculator: %w", err)
	}
	if err := os.WriteFile(filepath.Join(toolsDir, "example_text.py"), []byte(textCode), 0o644); err != nil {
		return fmt.Errorf("write text tool: %w", err)
	}

	fmt.Fprintln(os.Stderr, "Created example project with Python tools:")
	fmt.Fprintln(os.Stderr, "  tools/example_calculator.py")
	fmt.Fprintln(os.Stderr, "  tools/example_text.py")
	return nil
}

func scaffoldOpenAPI(dir string) error {
	apiDir := filepath.Join(dir, "api")
	if err := os.MkdirAll(apiDir, 0o755); err != nil {
		return fmt.Errorf("create api dir: %w", err)
	}

	spec := `openapi: "3.0.3"
info:
  title: Example User API
  version: "1.0.0"
  description: A simple User API for FolderMCP demonstration
paths:
  /users:
    get:
      operationId: list_users
      summary: List all users
      responses:
        "200":
          description: Successful response
          content:
            application/json:
              schema:
                type: array
                items:
                  $ref: "#/components/schemas/User"
    post:
      operationId: create_user
      summary: Create a new user
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/UserInput"
      responses:
        "201":
          description: User created
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/User"
  /users/{id}:
    get:
      operationId: get_user
      summary: Get a user by ID
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: Successful response
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/User"
components:
  schemas:
    User:
      type: object
      properties:
        id:
          type: string
        name:
          type: string
        email:
          type: string
    UserInput:
      type: object
      required:
        - name
        - email
      properties:
        name:
          type: string
        email:
          type: string
`

	if err := os.WriteFile(filepath.Join(apiDir, "example.yaml"), []byte(spec), 0o644); err != nil {
		return fmt.Errorf("write openapi spec: %w", err)
	}

	fmt.Fprintln(os.Stderr, "Created example project with OpenAPI spec:")
	fmt.Fprintln(os.Stderr, "  api/example.yaml")
	return nil
}

func scaffoldShell(dir string) error {
	toolsDir := filepath.Join(dir, "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		return fmt.Errorf("create tools dir: %w", err)
	}

	shellCode := `#!/usr/bin/env bash
# foldermcp:tool disk_usage
# foldermcp:description Show disk usage for the current directory
# foldermcp:risk read_only

du -sh "${1:-.}" 2>/dev/null || echo "Unable to determine disk usage"
`

	if err := os.WriteFile(filepath.Join(toolsDir, "example_disk_usage.sh"), []byte(shellCode), 0o755); err != nil {
		return fmt.Errorf("write shell tool: %w", err)
	}

	fmt.Fprintln(os.Stderr, "Created example project with shell tool:")
	fmt.Fprintln(os.Stderr, "  tools/example_disk_usage.sh")
	return nil
}
