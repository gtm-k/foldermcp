//go:build cgo

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	v3grpc "github.com/gtm-k/foldermcp/internal/v3/grpc"
)

var v3AuthCmd = &cobra.Command{
	Use:   "auth-v3",
	Short: "Manage v3.0 gRPC auth tokens and TLS certificates",
}

var v3AuthInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Generate token and self-signed TLS cert in ~/.foldermcp/auth/",
	RunE: func(cmd *cobra.Command, args []string) error {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		paths := v3grpc.DefaultAuthPaths(home)
		if err := v3grpc.Init(paths); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "auth initialized: %s\n", paths.Dir)
		return nil
	},
}

var v3AuthRotateCmd = &cobra.Command{
	Use:   "rotate",
	Short: "Rotate the auth token without re-indexing",
	RunE: func(cmd *cobra.Command, args []string) error {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		paths := v3grpc.DefaultAuthPaths(home)
		if err := v3grpc.Rotate(paths); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "token rotated: %s\n", paths.TokenFile)
		return nil
	},
}

func init() {
	v3AuthCmd.AddCommand(v3AuthInitCmd)
	v3AuthCmd.AddCommand(v3AuthRotateCmd)
	rootCmd.AddCommand(v3AuthCmd)
}
