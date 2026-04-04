package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/gtm-k/foldermcp/internal/state"
	"github.com/gtm-k/foldermcp/internal/workspace"
	"github.com/spf13/cobra"
)

var (
	catalogState string
	catalogRisk  string
	catalogType  string
)

var catalogCmd = &cobra.Command{
	Use:   "catalog",
	Short: "List all discovered tools in a table",
	Long:  `Displays a formatted table of all tools with their name, state, risk level, source file, and description.`,
	RunE:  runCatalog,
}

func init() {
	catalogCmd.Flags().StringVar(&catalogState, "state", "", "Filter by state (enabled, pending, disabled)")
	catalogCmd.Flags().StringVar(&catalogRisk, "risk", "", "Filter by risk (read_only, side_effects, destructive)")
	catalogCmd.Flags().StringVar(&catalogType, "type", "", "Filter by type (tool, resource)")
	rootCmd.AddCommand(catalogCmd)
}

func runCatalog(cmd *cobra.Command, args []string) error {
	dir, err := filepath.Abs(".")
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	ws, err := workspace.Open(dir)
	if err != nil {
		return fmt.Errorf("open workspace: %w", err)
	}

	store, err := state.Open(ws.LocalDir)
	if err != nil {
		return fmt.Errorf("open state store: %w", err)
	}
	defer func() { _ = store.Close() }()

	allTools, err := store.ListTools()
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}

	allResources, err := store.ListResources()
	if err != nil {
		return fmt.Errorf("list resources: %w", err)
	}

	// Apply filters.
	var tools []state.Tool
	for _, t := range allTools {
		if catalogType != "" && catalogType != "tool" {
			continue
		}
		if catalogState != "" && t.State != catalogState {
			continue
		}
		if catalogRisk != "" && t.Risk != catalogRisk {
			continue
		}
		tools = append(tools, t)
	}
	var resources []state.Resource
	for _, r := range allResources {
		if catalogType != "" && catalogType != "resource" {
			continue
		}
		if catalogState != "" && r.State != catalogState {
			continue
		}
		// Resources don't have a risk field; skip if risk filter is set.
		if catalogRisk != "" {
			continue
		}
		resources = append(resources, r)
	}

	if len(tools) == 0 && len(resources) == 0 {
		if jsonOutput {
			fmt.Println("[]")
			return nil
		}
		fmt.Fprintln(os.Stderr, "No tools or resources found. Run 'foldermcp init' first.")
		return nil
	}

	if jsonOutput {
		type catalogEntry struct {
			Name        string `json:"name"`
			Type        string `json:"type"`
			State       string `json:"state"`
			Risk        string `json:"risk,omitempty"`
			MimeType    string `json:"mime_type,omitempty"`
			SourceFile  string `json:"source_file,omitempty"`
			Description string `json:"description,omitempty"`
			SizeBytes   int64  `json:"size_bytes,omitempty"`
		}
		var entries []catalogEntry
		for _, t := range tools {
			entries = append(entries, catalogEntry{
				Name:        t.Name,
				Type:        "tool",
				State:       t.State,
				Risk:        t.Risk,
				SourceFile:  t.SourceFile,
				Description: t.Description,
			})
		}
		for _, r := range resources {
			entries = append(entries, catalogEntry{
				Name:      r.Name,
				Type:      "resource",
				State:     r.State,
				MimeType:  r.MimeType,
				SizeBytes: r.SizeBytes,
			})
		}
		data, err := json.MarshalIndent(entries, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal catalog: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "NAME\tTYPE\tSTATE\tRISK/MIME\tSOURCE\tDESCRIPTION"); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	for _, t := range tools {
		desc := truncateString(t.Description, 50)
		source := t.SourceFile
		if rel, err := filepath.Rel(dir, t.SourceFile); err == nil {
			source = rel
		}
		if _, err := fmt.Fprintf(w, "%s\ttool\t%s\t%s\t%s\t%s\n", t.Name, t.State, t.Risk, source, desc); err != nil {
			return fmt.Errorf("write tool row: %w", err)
		}
	}
	for _, r := range resources {
		sizeStr := formatSize(r.SizeBytes)
		source := filepath.Dir(r.FilePath)
		// Show relative source if possible.
		if rel, err := filepath.Rel(dir, source); err == nil {
			source = rel
		}
		if source == "." {
			source = "./"
		} else {
			source += "/"
		}
		if _, err := fmt.Fprintf(w, "%s\tresource\t%s\t%s\t%s\t%s\n", r.Name, r.State, r.MimeType, source, sizeStr); err != nil {
			return fmt.Errorf("write resource row: %w", err)
		}
	}
	return w.Flush()
}

func formatSize(bytes int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
	)
	switch {
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func truncateString(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}
