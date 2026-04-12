package main

import (
	"errors"

	"github.com/spf13/cobra"
)

var v3ServeCmd = &cobra.Command{
	Use:   "serve-v3",
	Short: "Run the v3.0 MCP query server (connects to indexer over gRPC)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return errors.New("foldermcp serve-v3: not yet implemented (Task E31)")
	},
}

func init() {
	rootCmd.AddCommand(v3ServeCmd)
}
