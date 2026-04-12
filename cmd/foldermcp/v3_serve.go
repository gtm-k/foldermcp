//go:build cgo

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	v3grpc "github.com/gtm-k/foldermcp/internal/v3/grpc"
	"github.com/gtm-k/foldermcp/internal/v3/store"
)

var v3ServeCmd = &cobra.Command{
	Use:   "serve-v3",
	Short: "Run the v3.0 gRPC query server (read-only, connects to indexer DB)",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return runV3Serve(ctx)
	},
}

func runV3Serve(ctx context.Context) error {
	storeDir := os.Getenv("FOLDERMCP_STORE")
	if storeDir == "" {
		home, _ := os.UserHomeDir()
		storeDir = filepath.Join(home, ".foldermcp", "store", "default")
	}
	dbPath := filepath.Join(storeDir, "index.db")

	db, err := store.Open(store.Options{Path: dbPath, Tier: store.DetectTier(detectRAMMB()), ReadOnly: true})
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	home, _ := os.UserHomeDir()
	sockPath := filepath.Join(home, ".foldermcp", "run", "serve.sock")

	listener, err := v3grpc.ListenUnixSocket(sockPath)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer func() { _ = listener.Close() }()

	srv := v3grpc.NewServer(v3grpc.ServerOpts{DB: db})
	fmt.Fprintf(os.Stderr, "foldermcp serve: listening on %s\n", sockPath)
	return srv.Serve(ctx, listener)
}

func init() {
	rootCmd.AddCommand(v3ServeCmd)
}
