//go:build cgo

package main

import (
	"context"
	"errors"

	"github.com/spf13/cobra"
)

var v3IndexCmd = &cobra.Command{
	Use:   "index-v3 [workspace]",
	Short: "Run the v3.0 indexer daemon (requires cgo build)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runV3Index(cmd.Context(), args[0])
	},
}

func runV3Index(ctx context.Context, workspacePath string) error {
	_ = ctx
	_ = workspacePath
	return errors.New("foldermcp index-v3: not yet implemented (Task C11+)")
}

func init() {
	rootCmd.AddCommand(v3IndexCmd)
}
