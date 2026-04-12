package main

import (
	"errors"

	"github.com/spf13/cobra"
)

var v3AuthCmd = &cobra.Command{
	Use:   "auth-v3",
	Short: "Manage v3.0 gRPC auth tokens and TLS certificates",
}

var v3AuthInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Generate token and self-signed TLS cert in ~/.foldermcp/auth/",
	RunE: func(cmd *cobra.Command, args []string) error {
		return errors.New("foldermcp auth-v3 init: not yet implemented (Task D27)")
	},
}

var v3AuthRotateCmd = &cobra.Command{
	Use:   "rotate",
	Short: "Rotate the auth token without re-indexing",
	RunE: func(cmd *cobra.Command, args []string) error {
		return errors.New("foldermcp auth-v3 rotate: not yet implemented (Task D27)")
	},
}

func init() {
	v3AuthCmd.AddCommand(v3AuthInitCmd)
	v3AuthCmd.AddCommand(v3AuthRotateCmd)
	rootCmd.AddCommand(v3AuthCmd)
}
