package main

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/foldermcp/foldermcp/internal/state"
	"github.com/spf13/cobra"
)

var catalogCmd = &cobra.Command{
	Use:   "catalog",
	Short: "List all discovered tools in a table",
	Long:  `Displays a formatted table of all tools with their name, state, risk level, source file, and description.`,
	RunE:  runCatalog,
}

func init() {
	rootCmd.AddCommand(catalogCmd)
}

func runCatalog(cmd *cobra.Command, args []string) error {
	dir, err := filepath.Abs(".")
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	stateDir := filepath.Join(dir, ".foldermcp")
	store, err := state.Open(stateDir)
	if err != nil {
		return fmt.Errorf("open state store: %w", err)
	}
	defer func() { _ = store.Close() }()

	tools, err := store.ListTools()
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}

	resources, err := store.ListResources()
	if err != nil {
		return fmt.Errorf("list resources: %w", err)
	}

	if len(tools) == 0 && len(resources) == 0 {
		fmt.Fprintln(os.Stderr, "No tools or resources found. Run 'foldermcp init' first.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "NAME\tTYPE\tSTATE\tRISK/MIME\tSOURCE\tDESCRIPTION"); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	for _, t := range tools {
		desc := truncateString(t.Description, 50)
		if _, err := fmt.Fprintf(w, "%s\ttool\t%s\t%s\t%s\t%s\n", t.Name, t.State, t.Risk, t.SourceFile, desc); err != nil {
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
