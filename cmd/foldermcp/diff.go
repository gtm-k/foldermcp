package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gtm-k/foldermcp/internal/config"
	"github.com/gtm-k/foldermcp/internal/introspect"
	"github.com/gtm-k/foldermcp/internal/state"
	"github.com/gtm-k/foldermcp/internal/workspace"
	"github.com/spf13/cobra"
)

var diffCmd = &cobra.Command{
	Use:   "diff",
	Short: "Show what would change if you re-scan",
	Long: `Scans the directory and compares discovered tools against the current state.
Shows new tools, removed tools, and modified tools (description or schema changed).`,
	RunE: runDiff,
}

func init() {
	rootCmd.AddCommand(diffCmd)
}

func runDiff(cmd *cobra.Command, args []string) error {
	dir, err := filepath.Abs(".")
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	ws, err := workspace.Open(dir)
	if err != nil {
		return fmt.Errorf("open workspace: %w", err)
	}

	cfg, err := config.Load(ws.ProjectDir)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	store, err := state.Open(ws.LocalDir)
	if err != nil {
		return fmt.Errorf("open state store: %w", err)
	}
	defer func() { _ = store.Close() }()

	// Load current tools from state.
	currentTools, err := store.ListTools()
	if err != nil {
		return fmt.Errorf("list current tools: %w", err)
	}

	// Scan directory for tools (without writing to state).
	registry := introspect.NewRegistry()
	discovered, err := registry.ScanDirectory(context.Background(), dir, cfg.Scan.Include, cfg.Scan.Exclude)
	if err != nil {
		return fmt.Errorf("scan directory: %w", err)
	}

	// Build maps for comparison.
	currentMap := make(map[string]state.Tool, len(currentTools))
	for _, t := range currentTools {
		currentMap[t.Name] = t
	}
	discoveredMap := make(map[string]introspect.ToolMetadata, len(discovered))
	for _, t := range discovered {
		discoveredMap[t.Name] = t
	}

	changes := 0

	// New tools: in discovered but not in current state.
	for _, d := range discovered {
		if _, exists := currentMap[d.Name]; !exists {
			_, _ = fmt.Fprintf(os.Stdout, "+ %s (%s)\n", d.Name, d.SourceFile)
			changes++
		}
	}

	// Removed tools: in current state but not in discovered.
	for _, c := range currentTools {
		if _, exists := discoveredMap[c.Name]; !exists {
			_, _ = fmt.Fprintf(os.Stdout, "- %s\n", c.Name)
			changes++
		}
	}

	// Load shared approvals for content hash comparison.
	approvals, _ := workspace.LoadApprovals(ws.ApprovalsPath())

	// Modified tools: in both, but description, schema, or content changed.
	for _, d := range discovered {
		c, exists := currentMap[d.Name]
		if !exists {
			continue
		}

		var reasons []string
		if d.Description != c.Description {
			reasons = append(reasons, "description changed")
		}

		// Normalize schemas for comparison.
		dSchema := d.InputSchema
		if dSchema == "" {
			dSchema = `{"type":"object","properties":{}}`
		}
		cSchema := c.InputSchema
		if cSchema == "" {
			cSchema = `{"type":"object","properties":{}}`
		}
		if !jsonEqual(dSchema, cSchema) {
			reasons = append(reasons, "schema changed")
		}

		// Check content hash: if the approvals store has a hash for this
		// tool (set at approval time), compare it to the current file.
		if approvals != nil {
			if entry, ok := approvals.Tools[d.Name]; ok && entry.ContentHash != "" {
				currentHash, err := workspace.HashFile(d.SourceFile)
				if err == nil && currentHash != entry.ContentHash {
					reasons = append(reasons, "content modified")
				}
			}
		}

		if len(reasons) > 0 {
			_, _ = fmt.Fprintf(os.Stdout, "~ %s (%s)\n", d.Name, joinReasons(reasons))
			changes++
		}
	}

	if changes == 0 {
		fmt.Fprintln(os.Stderr, "No changes detected.")
	} else {
		fmt.Fprintf(os.Stderr, "%d change(s) detected.\n", changes)
	}

	return nil
}

// jsonEqual compares two JSON strings for semantic equality.
func jsonEqual(a, b string) bool {
	var aVal, bVal interface{}
	if err := json.Unmarshal([]byte(a), &aVal); err != nil {
		return a == b
	}
	if err := json.Unmarshal([]byte(b), &bVal); err != nil {
		return a == b
	}
	aNorm, _ := json.Marshal(aVal)
	bNorm, _ := json.Marshal(bVal)
	return string(aNorm) == string(bNorm)
}

// joinReasons joins a slice of strings with ", ".
func joinReasons(reasons []string) string {
	result := ""
	for i, r := range reasons {
		if i > 0 {
			result += ", "
		}
		result += r
	}
	return result
}
