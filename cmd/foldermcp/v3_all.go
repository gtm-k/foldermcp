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
	"github.com/gtm-k/foldermcp/internal/v3/walker"
)

var v3AllCmd = &cobra.Command{
	Use:   "all-v3 [workspace]",
	Short: "Run indexer + gRPC server in one process over a Unix socket",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return runV3All(ctx, args[0])
	},
}

func runV3All(ctx context.Context, workspacePath string) error {
	storeDir := os.Getenv("FOLDERMCP_STORE")
	if storeDir == "" {
		home, _ := os.UserHomeDir()
		storeDir = filepath.Join(home, ".foldermcp", "store", "default")
	}
	if err := os.MkdirAll(storeDir, 0700); err != nil {
		return err
	}
	dbPath := filepath.Join(storeDir, "index.db")

	db, err := store.Open(store.Options{Path: dbPath, Tier: store.DetectTier(detectRAMMB())})
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	if err := store.Migrate(db, filepath.Join(storeDir, "backup", "pre-migration.db")); err != nil {
		return err
	}

	home, _ := os.UserHomeDir()
	sockPath := filepath.Join(home, ".foldermcp", "run", "serve.sock")
	listener, err := v3grpc.ListenUnixSocket(sockPath)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer func() { _ = listener.Close() }()

	srv := v3grpc.NewServer(v3grpc.ServerOpts{DB: db})

	// Kick off indexer pipeline in background
	go func() {
		fmt.Fprintf(os.Stderr, "foldermcp all: indexing %s\n", workspacePath)
		n, err := walker.Walk(ctx, db, walker.Options{Root: workspacePath})
		if err != nil {
			fmt.Fprintf(os.Stderr, "foldermcp all: walker error: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "foldermcp all: walker found %d files\n", n)
		}
	}()

	fmt.Fprintf(os.Stderr, "foldermcp all: serving on %s\n", sockPath)
	return srv.Serve(ctx, listener)
}

func init() {
	rootCmd.AddCommand(v3AllCmd)
}
