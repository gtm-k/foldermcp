//go:build cgo

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/gtm-k/foldermcp/internal/v3/pipeline"
	"github.com/gtm-k/foldermcp/internal/v3/store"
)

// watch-mode flags shared by index-v3 (and read by all-v3 via the same vars).
var (
	v3Watch         bool
	v3WatchInterval time.Duration
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

	// --watch (Phase 6 / D9, D14): after the initial full index, stay resident
	// and keep the index fresh as files change. The watch loop drives the same
	// Runner over changed files (events are candidates, not commands — the
	// sha256 trigger short-circuits mtime-only churn). Blocks until ctx is
	// cancelled (SIGINT/SIGTERM).
	if v3Watch {
		cfg := pipeline.WatchConfig{Interval: v3WatchInterval}
		loop := pipeline.NewWatchLoop(db, runner, workspacePath, cfg, nil, nil)
		fmt.Fprintf(os.Stderr, "foldermcp index: watching %s (interval %s)\n", workspacePath, cfg.Interval)
		if err := loop.Run(ctx); err != nil && ctx.Err() == nil {
			return fmt.Errorf("watch: %w", err)
		}
		fmt.Fprintf(os.Stderr, "foldermcp index: watch stopped\n")
	}
	return nil
}

// detectRAMMB returns estimated host RAM in MB.
// Minimal stub — real impl reads /proc/meminfo on Linux, sysctl on macOS.
func detectRAMMB() int {
	return 4096 // default to mid tier
}

func init() {
	v3IndexCmd.Flags().BoolVar(&v3Watch, "watch", false,
		"after the initial index, stay resident and re-index files as they change")
	v3IndexCmd.Flags().DurationVar(&v3WatchInterval, "watch-interval", 2*time.Second,
		"poll cadence for the watch-mode file scanner (e.g. 2s, 30s)")
	rootCmd.AddCommand(v3IndexCmd)
}
