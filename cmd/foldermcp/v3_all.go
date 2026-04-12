//go:build cgo

package main

import (
	"errors"

	"github.com/spf13/cobra"
)

var v3AllCmd = &cobra.Command{
	Use:   "all-v3 [workspace]",
	Short: "Run indexer + MCP server in one process over a Unix socket (prosumer tier)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return errors.New("foldermcp all-v3: not yet implemented (Task E31)")
	},
}

func init() {
	rootCmd.AddCommand(v3AllCmd)
}
