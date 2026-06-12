//go:build cgo

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"
	ort "github.com/yalue/onnxruntime_go"

	"github.com/gtm-k/foldermcp/internal/v3/chunker"
	"github.com/gtm-k/foldermcp/internal/v3/embed"
	"github.com/gtm-k/foldermcp/internal/v3/grammar"
	v3grpc "github.com/gtm-k/foldermcp/internal/v3/grpc"
	"github.com/gtm-k/foldermcp/internal/v3/pipeline"
	"github.com/gtm-k/foldermcp/internal/v3/store"
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

	configureOrtLib()
	modelPath, tokenizerPath, modelErr := resolveModelPaths()

	// Query-side embedder: a SEPARATE instance from the Runner's, so the
	// indexer goroutine never contends with gRPC query handlers for the
	// per-instance EmbedQuery lock. Sharing this one instance ACROSS
	// concurrent query handlers is safe: EmbedQuery is internally
	// serialized (E2 review finding 1 — gRPC handles RPCs on per-request
	// goroutines). Nil disables semantic search (server degrades to
	// FTS + filename per spec §9.5).
	var queryEmbedder *embed.Embedder
	if modelErr != nil {
		fmt.Fprintf(os.Stderr, "foldermcp all: WARNING semantic search disabled: %v\n", modelErr)
	} else {
		queryEmbedder = embed.NewEmbedder(modelPath, tokenizerPath)
	}
	srv := v3grpc.NewServer(v3grpc.ServerOpts{DB: db, Embedder: queryEmbedder})

	// Kick off indexer pipeline in background (D28b.3): walker →
	// structural → chunker → embeddings via pipeline.Runner.
	go func() {
		if modelErr != nil {
			fmt.Fprintf(os.Stderr, "foldermcp all: indexer not started: %v\n", modelErr)
			return
		}
		runner, err := newV3Runner(db, modelPath, tokenizerPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "foldermcp all: indexer init error: %v\n", err)
			return
		}
		fmt.Fprintf(os.Stderr, "foldermcp all: indexing %s\n", workspacePath)
		if err := runner.Run(ctx, workspacePath); err != nil {
			if errors.Is(err, context.Canceled) {
				fmt.Fprintf(os.Stderr, "foldermcp all: indexing interrupted by shutdown\n")
				return
			}
			fmt.Fprintf(os.Stderr, "foldermcp all: indexer error: %v\n", err)
			return
		}
		fmt.Fprintf(os.Stderr, "foldermcp all: indexing complete\n")
	}()

	fmt.Fprintf(os.Stderr, "foldermcp all: serving on %s\n", sockPath)
	return srv.Serve(ctx, listener)
}

// resolveModelPaths returns the model.onnx and tokenizer.json paths for
// the all-MiniLM-L6-v2 embedding model: $FOLDERMCP_MODEL_DIR if set,
// else ~/.foldermcp/models/all-MiniLM-L6-v2. An error means the model
// files are absent — callers decide whether that is fatal.
func resolveModelPaths() (modelPath, tokenizerPath string, err error) {
	modelDir := os.Getenv("FOLDERMCP_MODEL_DIR")
	if modelDir == "" {
		home, _ := os.UserHomeDir()
		modelDir = filepath.Join(home, ".foldermcp", "models", "all-MiniLM-L6-v2")
	}
	modelPath = filepath.Join(modelDir, "model.onnx")
	tokenizerPath = filepath.Join(modelDir, "tokenizer.json")
	for _, p := range []string{modelPath, tokenizerPath} {
		if _, statErr := os.Stat(p); statErr != nil {
			return "", "", fmt.Errorf("embedding model file missing at %s (set FOLDERMCP_MODEL_DIR or run `make v3-fetch-model`): %w", p, statErr)
		}
	}
	return modelPath, tokenizerPath, nil
}

// configureOrtLib points onnxruntime_go at a non-default ONNX Runtime
// shared library when FOLDERMCP_ORT_LIB is set (e.g.
// /usr/local/lib/libonnxruntime.so.1.24.1). The library's default is
// dlopen("onnxruntime.so") via the system search path. Must be called
// before the first Embedder init in the process.
func configureOrtLib() {
	if lib := os.Getenv("FOLDERMCP_ORT_LIB"); lib != "" {
		ort.SetSharedLibraryPath(lib)
	}
}

// newV3Runner assembles the four-pass pipeline orchestrator (D28b.3)
// with the production configuration: Go + Python grammars, cl100k_base
// token counting, and the default (fingerprint-bound) chunking policy.
func newV3Runner(db *sql.DB, modelPath, tokenizerPath string) (*pipeline.Runner, error) {
	goEx, err := grammar.NewGoExtractor()
	if err != nil {
		return nil, fmt.Errorf("go extractor: %w", err)
	}
	pyEx, err := grammar.NewPythonExtractor()
	if err != nil {
		return nil, fmt.Errorf("python extractor: %w", err)
	}
	return &pipeline.Runner{
		DB:         db,
		Embedder:   embed.NewEmbedder(modelPath, tokenizerPath),
		Writer:     embed.NewWriter(db),
		Extractors: map[string]grammar.Extractor{"go": goEx, "python": pyEx},
		Counter:    chunker.NewTiktokenCounter(),
		Cfg:        chunker.DefaultConfig(),
	}, nil
}

func init() {
	rootCmd.AddCommand(v3AllCmd)
}
