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
	defer store.Close()

	tools, err := store.ListTools()
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}

	if len(tools) == 0 {
		fmt.Fprintln(os.Stderr, "No tools found. Run 'foldermcp init' first.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATE\tRISK\tSOURCE\tDESCRIPTION")
	for _, t := range tools {
		desc := truncateString(t.Description, 50)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", t.Name, t.State, t.Risk, t.SourceFile, desc)
	}
	return w.Flush()
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
