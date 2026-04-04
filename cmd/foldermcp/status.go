package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/foldermcp/foldermcp/internal/state"
	"github.com/foldermcp/foldermcp/internal/workspace"
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

	ws, err := workspace.Open(dir)
	if err != nil {
		return fmt.Errorf("open workspace: %w", err)
	}

	store, err := state.Open(ws.LocalDir)
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
		if jsonOutput {
			fmt.Println("{}")
			return nil
		}
		fmt.Fprintln(os.Stderr, "No tools or resources found. Run 'foldermcp init' first.")
		return nil
	}

	// Count tools by state.
	stateCounts := map[string]int{}
	depStateCounts := map[string]int{}
	for _, t := range tools {
		stateCounts[t.State]++
		depStateCounts[t.DepState]++
	}

	// Count resources by state.
	resCounts := map[string]int{}
	for _, r := range resources {
		resCounts[r.State]++
	}

	if jsonOutput {
		statusObj := map[string]interface{}{
			"total_tools":     len(tools),
			"total_resources": len(resources),
			"tool_states":     stateCounts,
			"dep_states":      depStateCounts,
			"resource_states": resCounts,
		}
		data, err := json.MarshalIndent(statusObj, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal status: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	_, _ = fmt.Fprintf(os.Stdout, "FolderMCP Status (%d tools, %d resources)\n", len(tools), len(resources))
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

	if len(resources) > 0 {
		_, _ = fmt.Fprintln(os.Stdout, "")
		_, _ = fmt.Fprintln(os.Stdout, "Resource States:")
		printCount(resCounts, "enabled")
		printCount(resCounts, "pending")
		printCount(resCounts, "disabled")
	}

	return nil
}

func printCount(counts map[string]int, key string) {
	count := counts[key]
	if count > 0 {
		_, _ = fmt.Fprintf(os.Stdout, "  %-24s %d\n", key, count)
	}
}
