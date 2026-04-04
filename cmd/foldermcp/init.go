package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/foldermcp/foldermcp/internal/config"
	"github.com/foldermcp/foldermcp/internal/introspect"
	"github.com/foldermcp/foldermcp/internal/state"
	"github.com/foldermcp/foldermcp/internal/workspace"
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
