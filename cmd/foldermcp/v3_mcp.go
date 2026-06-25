package main

import (
	v3mcp "github.com/gtm-k/foldermcp/internal/v3/mcp"
	"github.com/spf13/cobra"
)

var v3MCPCmd = &cobra.Command{
	Use:   "mcp-v3",
	Short: "Run the v3.0 MCP stdio shim (launched by Claude Desktop per session)",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		shim, err := v3mcp.NewShim(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = shim.Close() }()
		return shim.Serve()
	},
}

func init() {
	rootCmd.AddCommand(v3MCPCmd)
}
