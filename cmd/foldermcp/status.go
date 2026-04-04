package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/foldermcp/foldermcp/internal/state"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show summary of tool states and dependency states",
	Long:  `Displays a count of tools grouped by their approval state and dependency resolution state.`,
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
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

	if len(tools) == 0 {
		fmt.Fprintln(os.Stderr, "No tools found. Run 'foldermcp init' first.")
		return nil
	}

	// Count by state.
	stateCounts := map[string]int{}
	depStateCounts := map[string]int{}
	for _, t := range tools {
		stateCounts[t.State]++
		depStateCounts[t.DepState]++
	}

	_, _ = fmt.Fprintf(os.Stdout, "FolderMCP Status (%d tools)\n", len(tools))
	_, _ = fmt.Fprintln(os.Stdout, "")
	_, _ = fmt.Fprintln(os.Stdout, "Tool States:")
	printCount(stateCounts, "enabled")
	printCount(stateCounts, "pending")
	printCount(stateCounts, "disabled")
	printCount(stateCounts, "requires_confirmation")

	_, _ = fmt.Fprintln(os.Stdout, "")
	_, _ = fmt.Fprintln(os.Stdout, "Dependency States:")
	printCount(depStateCounts, "resolved")
	printCount(depStateCounts, "resolving")
	printCount(depStateCounts, "failed")

	return nil
}

func printCount(counts map[string]int, key string) {
	count := counts[key]
	if count > 0 {
		_, _ = fmt.Fprintf(os.Stdout, "  %-24s %d\n", key, count)
	}
}
