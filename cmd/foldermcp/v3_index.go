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

	"github.com/gtm-k/foldermcp/internal/v3/store"
)

var v3IndexCmd = &cobra.Command{
	Use:   "index-v3 [workspace]",
	Short: "Run the v3.0 indexer daemon (requires cgo build)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return runV3Index(ctx, args[0])
	},
}

func runV3Index(ctx context.Context, workspacePath string) error {
	storeDir := os.Getenv("FOLDERMCP_STORE")
	if storeDir == "" {
		home, _ := os.UserHomeDir()
		storeDir = filepath.Join(home, ".foldermcp", "store", "default")
	}
	if err := os.MkdirAll(storeDir, 0700); err != nil {
		return fmt.Errorf("mkdir store: %w", err)
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

	// D28b.3: full four-pass pipeline (walker → structural → chunker →
	// embeddings) via pipeline.Runner — not just the walker.
	configureOrtLib()
	modelPath, tokenizerPath, err := resolveModelPaths()
	if err != nil {
		return err
	}
	runner, err := newV3Runner(db, modelPath, tokenizerPath)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "foldermcp index: indexing %s\n", workspacePath)
	if err := runner.Run(ctx, workspacePath); err != nil {
		return fmt.Errorf("index: %w", err)
	}
	fmt.Fprintf(os.Stderr, "foldermcp index: indexing complete\n")
	return nil
}

// detectRAMMB returns estimated host RAM in MB.
// Minimal stub — real impl reads /proc/meminfo on Linux, sysctl on macOS.
func detectRAMMB() int {
	return 4096 // default to mid tier
}

func init() {
	rootCmd.AddCommand(v3IndexCmd)
}
