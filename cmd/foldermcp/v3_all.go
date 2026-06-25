//go:build cgo

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	ort "github.com/yalue/onnxruntime_go"

	"github.com/gtm-k/foldermcp/internal/v3/chunker"
	"github.com/gtm-k/foldermcp/internal/v3/embed"
	"github.com/gtm-k/foldermcp/internal/v3/grammar"
	v3grpc "github.com/gtm-k/foldermcp/internal/v3/grpc"
	"github.com/gtm-k/foldermcp/internal/v3/grpc/admin"
	"github.com/gtm-k/foldermcp/internal/v3/pipeline"
	"github.com/gtm-k/foldermcp/internal/v3/store"
	"github.com/gtm-k/foldermcp/internal/v3/transport"
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

// v3AllWatch / v3AllWatchInterval mirror the index-v3 --watch flags for the
// combined daemon. (Separate vars so the two commands' flag sets stay
// independent under cobra.)
var (
	v3AllWatch         bool
	v3AllWatchInterval time.Duration
)

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

	sockPath := transport.DefaultSocketPath()
	listener, err := transport.Listen(sockPath)
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

	// Read-side fingerprint gate (mirror of the indexer's EnsureFingerprint).
	// On a fresh index there is no fingerprint yet (the background indexer
	// writes it), so this is a no-op; on an EXISTING incompatible index (e.g.
	// an old per-vector "int8" store the indexer goroutine will refuse to
	// re-embed) it disables semantic search so the server degrades to lexical
	// with an observable reason instead of serving garbage rankings.
	if queryEmbedder != nil {
		if ok, stored := embed.SemanticIndexCompatible(db); !ok {
			fmt.Fprintf(os.Stderr, "foldermcp all: WARNING semantic search disabled — "+
				"index quantization %q is incompatible with this binary (%q); "+
				"delete the store's index.db and re-run to rebuild\n",
				stored, embed.QuantizationModeString())
			queryEmbedder = nil
		}
	}
	// FIX D: allocate the watch metric object BEFORE building the server so the
	// SAME pointer is handed to both NewServer (Status reads it) and
	// NewWatchLoop (the loop writes it). The metric's fields are atomic, so
	// sharing across the gRPC handler and the watch goroutine is safe. Passed
	// (and thus surfaced in Status) only in --watch mode; nil otherwise so the
	// headless/non-watch path does not advertise watch counters.
	var watchMetric *pipeline.WatchMetrics
	var watchProvider admin.WatchMetricsProvider
	if v3AllWatch {
		watchMetric = &pipeline.WatchMetrics{}
		watchProvider = watchMetric
	}
	srv := v3grpc.NewServer(v3grpc.ServerOpts{DB: db, Embedder: queryEmbedder, Watch: watchProvider})

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

		// --watch (Phase 6): keep the index fresh while the server serves.
		// Backpressure is built into the loop (one Runner pass at a time), so
		// the indexer goroutine never spawns unbounded work. The query-side
		// embedder is a separate instance, so re-index writes never contend with
		// query reads for the EMBED lock.
		//
		// CAVEAT (FIX E): store.Open sets SetMaxOpenConns(1), so the gRPC query
		// path and the watch-loop writer share ONE database connection. Under an
		// edit storm, queries therefore BLOCK for the duration of each per-file
		// Runner transaction — they do not run concurrently with re-index writes
		// at the SQLite layer. Removing that serialization needs a WAL reader pool
		// (separate read-only connections); that is a tracked architectural
		// follow-up, not addressed here.
		if v3AllWatch {
			cfg := pipeline.WatchConfig{Interval: v3AllWatchInterval}
			loop := pipeline.NewWatchLoop(db, runner, workspacePath, cfg, watchMetric, nil)
			fmt.Fprintf(os.Stderr, "foldermcp all: watching %s (interval %s)\n", workspacePath, cfg.Interval)
			if err := loop.Run(ctx); err != nil && ctx.Err() == nil {
				fmt.Fprintf(os.Stderr, "foldermcp all: watch error: %v\n", err)
			}
			fmt.Fprintf(os.Stderr, "foldermcp all: watch stopped\n")
		}
	}()

	fmt.Fprintf(os.Stderr, "foldermcp all: serving on %s\n", sockPath)
	return srv.Serve(ctx, listener)
}

// exeDir returns the directory of the running executable, or "" if it cannot
// be determined. Used to locate bundled dependencies (ONNX lib, model) shipped
// alongside the binary in a self-contained archive.
func exeDir() string {
	p, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(p)
}

// resolveModelPaths returns the model.onnx and tokenizer.json paths for the
// all-MiniLM-L6-v2 embedding model, in priority order:
//  1. $FOLDERMCP_MODEL_DIR if set,
//  2. a "model/" directory beside the executable (self-contained release bundle),
//  3. ~/.foldermcp/models/all-MiniLM-L6-v2.
//
// An error means the model files are absent — callers decide whether that is
// fatal.
func resolveModelPaths() (modelPath, tokenizerPath string, err error) {
	modelDir := os.Getenv("FOLDERMCP_MODEL_DIR")
	if modelDir == "" {
		if d := exeDir(); d != "" {
			cand := filepath.Join(d, "model")
			// Require BOTH files beside the executable before committing to the
			// bundle dir — otherwise a partial bundle would block the home-dir
			// fallback and hard-error (Codex review).
			_, e1 := os.Stat(filepath.Join(cand, "model.onnx"))
			_, e2 := os.Stat(filepath.Join(cand, "tokenizer.json"))
			if e1 == nil && e2 == nil {
				modelDir = cand
			}
		}
	}
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

// configureOrtLib points onnxruntime_go at the ONNX Runtime shared library, in
// priority order (see resolveOrtLib):
//  1. $FOLDERMCP_ORT_LIB if set,
//  2. an onnxruntime shared lib beside the executable (self-contained bundle),
//  3. an onnxruntime shared lib in an ort/ subdir beside the executable
//     (bundle/dev layout, e.g. the repo's bin/ort/),
//  4. otherwise the library's default dlopen("onnxruntime.so") via the system
//     search path — logged at WARN, because that path can resolve to an
//     unrelated, version-incompatible install.
//
// Must be called before the first Embedder init in the process. The selected
// library (and why) is logged so an "ORT API version" mismatch downstream is
// traceable to the exact file.
//
// Trust note: the exe-relative lookup assumes the install directory is no less
// trusted than the executable itself (single-user bundle). In a shared install
// dir that is writable by a lower-privileged principal than the binary's owner,
// prefer setting FOLDERMCP_ORT_LIB to a vetted absolute path.
func configureOrtLib() {
	// Resolution logic lives in resolveOrtLib (pure, unit-tested). It probes,
	// in order: $FOLDERMCP_ORT_LIB, the lib beside the exe, then an ort/ subdir
	// beside the exe. Only the platform-correct library name is considered, so a
	// stray Windows DLL left beside a Linux/macOS binary is never selected.
	lib, source := resolveOrtLib(
		os.Getenv("FOLDERMCP_ORT_LIB"),
		exeDir(),
		runtime.GOOS,
		// Require a regular file (os.Stat follows symlinks, so a symlink to a
		// real lib still qualifies) — a directory or other object named like the
		// lib must not short-circuit the ort/ subdir + system fallbacks.
		func(p string) bool { fi, err := os.Stat(p); return err == nil && fi.Mode().IsRegular() },
	)
	if lib != "" {
		// Observability: record which library (and why) was selected so a
		// downstream "ORT API version" mismatch is traceable to the exact file.
		slog.Info("ort: using ONNX Runtime shared library", "path", lib, "source", source)
		ort.SetSharedLibraryPath(lib)
		return
	}
	// source == "system": nothing bundled beside the executable. The OS loader
	// will dlopen onnxruntime by name from its default search path, which can
	// resolve to an unrelated, possibly incompatible install (the exact failure
	// QA hit: a stray ORT 1.17.1 on PATH vs. the binary's required API). Warn so
	// it is diagnosable; set FOLDERMCP_ORT_LIB to pin a vetted library.
	slog.Warn("ort: no bundled ONNX Runtime found beside the executable; falling back to the system loader",
		"hint", "set FOLDERMCP_ORT_LIB to pin a specific library")
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
	v3AllCmd.Flags().BoolVar(&v3AllWatch, "watch", false,
		"after the initial index, keep the index fresh as files change")
	v3AllCmd.Flags().DurationVar(&v3AllWatchInterval, "watch-interval", 2*time.Second,
		"poll cadence for the watch-mode file scanner (e.g. 2s, 30s)")
	rootCmd.AddCommand(v3AllCmd)
}
