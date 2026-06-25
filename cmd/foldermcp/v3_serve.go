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

	"github.com/gtm-k/foldermcp/internal/v3/embed"
	v3grpc "github.com/gtm-k/foldermcp/internal/v3/grpc"
	"github.com/gtm-k/foldermcp/internal/v3/store"
	"github.com/gtm-k/foldermcp/internal/v3/transport"
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

	sockPath := transport.DefaultSocketPath()

	listener, err := transport.Listen(sockPath)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer func() { _ = listener.Close() }()

	// Query-side embedder, mirroring all-v3 (E2 review finding 5): before
	// this wiring, serve-v3 silently ran lexical-only even with the model
	// installed — a permanent degradation with no observable signal. Safe
	// to share across concurrent gRPC handlers because EmbedQuery is
	// internally serialized. Nil disables semantic search (server degrades
	// to FTS + filename per spec §9.5) — and says so on stderr.
	configureOrtLib()
	var queryEmbedder *embed.Embedder
	if modelPath, tokenizerPath, modelErr := resolveModelPaths(); modelErr != nil {
		fmt.Fprintf(os.Stderr, "foldermcp serve: WARNING semantic search disabled: %v\n", modelErr)
	} else {
		queryEmbedder = embed.NewEmbedder(modelPath, tokenizerPath)
	}

	// Read-side fingerprint gate (mirror of the indexer's EnsureFingerprint):
	// refuse to run fixed-scale query codes against a store quantized with a
	// different scheme — that would silently return garbage rankings. Degrade
	// to lexical (FTS + filename) and say why, rather than serve nonsense.
	if queryEmbedder != nil {
		if ok, stored := embed.SemanticIndexCompatible(db); !ok {
			fmt.Fprintf(os.Stderr, "foldermcp serve: WARNING semantic search disabled — "+
				"index quantization %q is incompatible with this binary (%q); "+
				"delete the store's index.db and re-run index-v3 to rebuild\n",
				stored, embed.QuantizationModeString())
			queryEmbedder = nil
		}
	}

	srv := v3grpc.NewServer(v3grpc.ServerOpts{DB: db, Embedder: queryEmbedder})
	fmt.Fprintf(os.Stderr, "foldermcp serve: listening on %s\n", sockPath)
	return srv.Serve(ctx, listener)
}

func init() {
	rootCmd.AddCommand(v3ServeCmd)
}
