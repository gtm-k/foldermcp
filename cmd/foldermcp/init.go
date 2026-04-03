package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/foldermcp/foldermcp/internal/config"
	"github.com/foldermcp/foldermcp/internal/introspect"
	"github.com/foldermcp/foldermcp/internal/state"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init [path]",
	Short: "Initialize a directory as a FolderMCP workspace",
	Long: `Scans the target directory for Python scripts and OpenAPI specs,
creates a foldermcp.yaml config (if missing), and populates the state store
with discovered tools.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runInit,
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
		return fmt.Errorf("resolve path: %w", err)
	}

	// Load or create config.
	cfg, err := config.Load(absDir)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Save config if it doesn't exist yet.
	configPath := filepath.Join(absDir, "foldermcp.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := config.Save(absDir, cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		fmt.Fprintln(os.Stderr, "Created foldermcp.yaml")
	}

	// Open state store.
	stateDir := filepath.Join(absDir, ".foldermcp")
	store, err := state.Open(stateDir)
	if err != nil {
		return fmt.Errorf("open state store: %w", err)
	}
	defer store.Close()

	// Scan directory.
	start := time.Now()
	registry := introspect.NewRegistry()
	tools, err := registry.ScanDirectory(absDir, cfg.Scan.Include, cfg.Scan.Exclude)
	if err != nil {
		return fmt.Errorf("scan directory: %w", err)
	}
	elapsed := time.Since(start)

	// Upsert each discovered tool into state store.
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

		t := state.Tool{
			Name:        tm.Name,
			SourceFile:  tm.SourceFile,
			Description: tm.Description,
			InputSchema: schemaJSON,
			Risk:        risk,
			State:       toolState,
		}
		if err := store.UpsertTool(t); err != nil {
			return fmt.Errorf("upsert tool %q: %w", tm.Name, err)
		}
	}

	fmt.Fprintf(os.Stderr, "Found %d tools in %dms\n", len(tools), elapsed.Milliseconds())
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Next steps:")
	fmt.Fprintln(os.Stderr, "  foldermcp review    — approve or disable discovered tools")
	fmt.Fprintln(os.Stderr, "  foldermcp catalog   — list all tools and their states")
	fmt.Fprintln(os.Stderr, "  foldermcp serve     — start the MCP server")

	return nil
}
