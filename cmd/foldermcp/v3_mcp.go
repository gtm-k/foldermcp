package main

import (
	"errors"

	"github.com/spf13/cobra"
)

var v3MCPCmd = &cobra.Command{
	Use:   "mcp-v3",
	Short: "Run the v3.0 MCP stdio shim (launched by Claude Desktop per session)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return errors.New("foldermcp mcp-v3: not yet implemented (Task F32)")
	},
}

func init() {
	rootCmd.AddCommand(v3MCPCmd)
}
