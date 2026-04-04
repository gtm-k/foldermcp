package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/foldermcp/foldermcp/internal/export"
	"github.com/foldermcp/foldermcp/internal/state"
	"github.com/spf13/cobra"
)

var exportCmd = &cobra.Command{
	Use:   "export <format>",
	Short: "Export compatibility artifacts",
	Long: `Exports project metadata in various formats for interoperability.

Supported formats:
  a2a    Generate an A2A-compatible agent-card.json`,
	Args: cobra.ExactArgs(1),
	RunE: runExport,
}

func init() {
	exportCmd.Flags().String("name", "", "agent name (defaults to directory name)")
	exportCmd.Flags().String("version", "0.1.0", "agent version")
	exportCmd.Flags().String("url", "http://localhost:3000", "agent URL")
	rootCmd.AddCommand(exportCmd)
}

func runExport(cmd *cobra.Command, args []string) error {
	format := args[0]

	switch format {
	case "a2a":
		return exportA2A(cmd)
	default:
		return fmt.Errorf("unsupported format %q; supported: a2a", format)
	}
}

func exportA2A(cmd *cobra.Command) error {
	dir, err := filepath.Abs(".")
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	name, _ := cmd.Flags().GetString("name")
	if name == "" {
		name = filepath.Base(dir)
	}
	version, _ := cmd.Flags().GetString("version")
	url, _ := cmd.Flags().GetString("url")

	// Try to load tools from the state store.
	stateDir := filepath.Join(dir, ".foldermcp")
	var tools []state.Tool

	store, err := state.Open(stateDir)
	if err == nil {
		defer func() { _ = store.Close() }()
		tools, err = store.ListTools()
		if err != nil {
			return fmt.Errorf("list tools: %w", err)
		}
	}
	// If no store exists yet, generate with empty capabilities.

	card := export.GenerateAgentCard(name, version, url, tools)

	if err := export.WriteAgentCard(dir, card); err != nil {
		return fmt.Errorf("write agent card: %w", err)
	}

	outPath := filepath.Join(dir, "agent-card.json")
	fmt.Fprintf(os.Stderr, "Wrote %s\n", outPath)
	fmt.Fprintf(os.Stderr, "Agent: %s v%s (%d capabilities)\n", card.Name, card.Version, len(card.Capabilities))

	return nil
}
