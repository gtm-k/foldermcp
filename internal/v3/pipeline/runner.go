//go:build cgo

package pipeline

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gtm-k/foldermcp/internal/v3/chunker"
	"github.com/gtm-k/foldermcp/internal/v3/embed"
	"github.com/gtm-k/foldermcp/internal/v3/grammar"
	"github.com/gtm-k/foldermcp/internal/v3/walker"
)

// embedder is the narrow interface the Runner consumes. Concrete type
// *embed.Embedder satisfies this (via the exported Init wrapper added
// under D28b D10). Tests pass a stub that records inputs, returns a
// deterministic 384-byte vector, and can inject Init failures without
// touching ONNX Runtime.
type embedder interface {
	Init() error                            // one-shot session init; fatal if it returns error
	EmbedQuery(text string) ([]byte, error) // returns 384 bytes, int8-quantised
}

const (
	// defaultEmbedBatch is the embed-batch buffer size (D28b D1).
	defaultEmbedBatch = 32
	// pendingFetchSize is the minimum PendingFiles batch per loop turn.
	pendingFetchSize = 100
	// maxReadBytes caps per-file reads. Files larger than this are marked
	// failed (oversize) and skipped — they would blow out tokenizer
	// memory and dominate the 1-TB budget (pre-mortem Story 2 includes a
	// 40 MB generated .py file as a failure class).
	maxReadBytes = 32 * 1024 * 1024 // 32 MB — covers ~99% of real code/prose
)

// Runner orchestrates the four-pass pipeline (walker → structural →
// chunker → embeddings) per file: Option A, single goroutine, single
// Embedder (D28b D1; multi-worker is v4.0 scope, D2).
type Runner struct {
	DB         *sql.DB
	Embedder   embedder      // interface, not concrete type (§4.2.1)
	Writer     *embed.Writer // fingerprint-guarded vec0 writer
	Extractors map[string]grammar.Extractor
	Counter    chunker.Counter
	Cfg        chunker.Config
	BatchSize  int          // embed batch size; default 32
	Logger     *slog.Logger // default slog.Default()

	// currentPass tracks which pass the in-flight file is in so failures
	// (including panics) are attributed to the right pipeline_state row.
	// Runner is single-goroutine by design — no synchronization needed.
	currentPass PassName
	embedBuf    []pendingChunk
	failedFiles int
}

type pendingChunk struct {
	chunkID int64
	text    string
}

type fileRow struct {
	ID           int64
	Path         string
	Size         int64
	Mime         string
	ContentClass string
}

// Run executes the pipeline against all files under root. Honors ctx
// cancellation between files (each file's rows commit in one atomic
// transaction, so there is never a cross-file batch to drain) and
// returns the first fatal (non-per-file) error, or nil if the run
// completed (per-file failures are recorded in pipeline_state).
func (r *Runner) Run(ctx context.Context, root string) error {
	logger := r.logger()
	if r.DB == nil || r.Embedder == nil || r.Writer == nil || r.Counter == nil {
		return errors.New("runner init: DB, Embedder, Writer, and Counter are required")
	}
	if r.Cfg == (chunker.Config{}) {
		r.Cfg = chunker.DefaultConfig()
	}
	// Policy drift guard (pre-mortem Story 3, D28b-E2 follow-up): the
	// Writer's fingerprint records chunker.DefaultConfig().PolicyString().
	// Chunking under any other policy would persist chunks the
	// fingerprint misdescribes — fail fast before any work happens.
	if want := chunker.DefaultConfig().PolicyString(); r.Cfg.PolicyString() != want {
		return fmt.Errorf("runner init: chunking policy %q does not match fingerprint policy %q — embedding_fingerprint would misdescribe persisted chunks", r.Cfg.PolicyString(), want)
	}
	startTime := time.Now().Unix()
	// Story 1 metric: grep-able start line before any per-file work.
	logger.Info("runner: start", "root", root,
		"embed_batch_size", r.batchSize(), "chunking_policy", r.Cfg.PolicyString())

	// Step 2: walker enumerates files. Its ON CONFLICT DO UPDATE fires
	// the migration-0002 trigger for any file whose sha256 changed since
	// the last run, clearing its downstream state atomically (D8 Layer 1).
	if _, err := walker.Walk(ctx, r.DB, walker.Options{Root: root}); err != nil {
		return fmt.Errorf("runner: walk: %w", err)
	}

	// Step 3: deletion reconciliation (Phase C Codex item #4) — files not
	// seen by this walk disappeared from disk.
	if _, err := r.DB.ExecContext(ctx,
		`UPDATE files SET deleted_at = ? WHERE last_seen < ? AND deleted_at IS NULL`,
		startTime, startTime); err != nil {
		return fmt.Errorf("runner: deletion reconciliation: %w", err)
	}

	// Step 4: embedder init + fingerprint check — FATAL on error
	// (pre-mortem Story 1: a lazy init inside per-file recovery would
	// swallow the same error once per file and exit 0 with zero vectors).
	if err := r.Writer.EnsureFingerprint(); err != nil {
		return fmt.Errorf("runner init: fingerprint: %w", err)
	}
	if err := r.Embedder.Init(); err != nil {
		return fmt.Errorf("runner init: embedder: %w", err)
	}

	// Step 5: main loop over PendingFiles(PassEmbeddings) — the terminal
	// pass is the selector (D9): under the clean-slate contract a file is
	// either fully done (skip) or processed from scratch.
	//
	// Files that fail (or are skipped) keep a non-'done' terminal row, so
	// PendingFiles keeps returning them within this run. The attempted
	// set makes each file processed at most once per Run; growing the
	// fetch limit by len(attempted) guarantees that any never-attempted
	// pending file still fits in the batch (at most len(attempted) of the
	// returned rows can be retries), so the loop terminates exactly when
	// every pending file has been attempted.
	attempted := make(map[int64]bool)
	r.failedFiles = 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		ids, err := PendingFiles(ctx, r.DB, PassEmbeddings, len(attempted)+pendingFetchSize)
		if err != nil {
			return fmt.Errorf("runner: pending files: %w", err)
		}
		progressed := false
		for _, id := range ids {
			if attempted[id] {
				continue
			}
			attempted[id] = true
			progressed = true
			if err := ctx.Err(); err != nil {
				return err
			}
			r.processOneFile(ctx, id)
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if !progressed {
			break
		}
	}

	// Step 6: end-of-Run failure surfacing (pre-mortem Story 2).
	r.logSummary(ctx, len(attempted))
	return nil
}

// processOneFile wraps runPerFile with panic recovery and failure
// recording so one bad file never stops the run (D28b.1 step 5).
func (r *Runner) processOneFile(ctx context.Context, fileID int64) {
	r.currentPass = PassStructural
	defer func() {
		if p := recover(); p != nil {
			r.recordFailure(ctx, fileID, r.currentPass, fmt.Errorf("panic: %v", p))
			r.logger().Error("runner: per-file panic", "file_id", fileID, "panic", p)
		}
	}()

	f, err := r.loadFileRow(ctx, fileID)
	if err != nil {
		if ctx.Err() != nil {
			return // cancellation, not a per-file failure
		}
		r.recordFailure(ctx, fileID, PassStructural, fmt.Errorf("load file row: %w", err))
		return
	}
	if err := r.runPerFile(ctx, f); err != nil {
		if ctx.Err() != nil {
			return // cancellation, not a per-file failure
		}
		r.recordFailure(ctx, fileID, r.currentPass, err)
	}
}

// runPerFile processes one file through structural → chunker →
// embeddings inside a single transaction (§4.1 atomicity: all of a
// file's rows commit together or not at all).
func (r *Runner) runPerFile(ctx context.Context, f fileRow) (err error) {
	r.embedBuf = r.embedBuf[:0]

	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		// Panic-aware: on a panic (tree-sitter edge cases, pre-mortem
		// Story 2) the named return err is still nil — committing here
		// would persist a half-processed file. Roll back and re-panic so
		// processOneFile's recover records the failure.
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback()
		} else {
			err = tx.Commit()
		}
	}()

	// Layer 2 defensive delete (§4.1 + Amendment 2026-06-11): even if the
	// Layer 1 trigger were bypassed (direct SQL, test fixtures, future
	// walker change), Runner guarantees clean slate per entry. Order is
	// load-bearing: embeddings is a vec0 virtual table outside the FK
	// graph, so its delete must run while the chunk rows that identify
	// the orphaned vectors still exist; deleting nodes first would
	// cascade-delete chunks and make the orphans unidentifiable forever.
	if _, err = tx.ExecContext(ctx, `DELETE FROM embeddings WHERE chunk_id IN (
  SELECT chunk_id FROM chunks WHERE node_id IN (
    SELECT node_id FROM nodes WHERE file_id = ?))`, f.ID); err != nil {
		return fmt.Errorf("runner: defensive cleanup embeddings: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM pipeline_state WHERE file_id = ?`, f.ID); err != nil {
		return fmt.Errorf("runner: defensive cleanup pipeline_state: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM nodes WHERE file_id = ?`, f.ID); err != nil {
		return fmt.Errorf("runner: defensive cleanup nodes: %w", err)
	}

	// Non-indexable classes (image/media/data/unknown): no OCR/whisper in
	// M1. Mark all three passes skipped so status surfaces them honestly
	// instead of leaving them invisible (actor-observability).
	if f.ContentClass != "code" && f.ContentClass != "document" {
		for _, p := range []PassName{PassStructural, PassChunker, PassEmbeddings} {
			if err = markStatusTx(ctx, tx, f.ID, p, StatusSkipped, ""); err != nil {
				return err
			}
		}
		return nil
	}

	// Binary documents (D28b-E2 follow-up): content_class 'document'
	// includes .pdf/.docx/.odt, whose raw bytes M1 cannot extract text
	// from — routing them through the prose chunker produces garbage
	// chunks. Skip with a grep-able marker; M2 Phase 4 replaces this
	// with real PDF extraction. Plain-text documents (.md/.txt/.rst/...)
	// fall through to prose chunking as before.
	if f.ContentClass == "document" && isBinaryDocument(f.Path) {
		for _, p := range []PassName{PassStructural, PassChunker, PassEmbeddings} {
			if err = markStatusTx(ctx, tx, f.ID, p, StatusSkipped, "binary_document_pending_m2"); err != nil {
				return err
			}
		}
		return nil
	}

	r.currentPass = PassStructural
	content, err := readFileContent(f.Path, f.Size)
	if err != nil {
		return err
	}

	// Pass 1: structural.
	symNodes, fileNodeID, syms, err := r.runStructural(ctx, tx, f, content)
	if err != nil {
		return err
	}

	// Pass 2: chunker.
	r.currentPass = PassChunker
	chunks, err := r.runChunker(ctx, tx, f, content, symNodes, fileNodeID, syms)
	if err != nil {
		return err
	}

	// Pass 3: embeddings — buffered in 32-chunk batches (D1), drained
	// before commit so all of the file's rows land in one transaction.
	r.currentPass = PassEmbeddings
	if err = markStatusTx(ctx, tx, f.ID, PassEmbeddings, StatusRunning, ""); err != nil {
		return err
	}
	for _, c := range chunks {
		r.embedBuf = append(r.embedBuf, c)
		if len(r.embedBuf) >= r.batchSize() {
			if err = r.flushEmbedBatch(tx); err != nil {
				return err
			}
		}
	}
	if err = r.flushEmbedBatch(tx); err != nil { // drain remainder
		return err
	}
	if err = markStatusTx(ctx, tx, f.ID, PassEmbeddings, StatusDone, ""); err != nil {
		return err
	}
	return nil
}

// runStructural extracts symbols (code with a supported grammar) or
// creates a single file node (prose; code without grammar; code with
// zero extracted symbols). Returns the symbol→node_id map, the fallback
// file node id (0 when symbol nodes exist), and the extracted symbols.
func (r *Runner) runStructural(ctx context.Context, tx *sql.Tx, f fileRow, content []byte) (map[grammar.Symbol]int64, int64, []grammar.Symbol, error) {
	if err := markStatusTx(ctx, tx, f.ID, PassStructural, StatusRunning, ""); err != nil {
		return nil, 0, nil, err
	}
	now := time.Now().Unix()

	var syms []grammar.Symbol
	ext := r.extractorFor(f)
	if ext != nil {
		var err error
		syms, err = ext.Extract(content)
		if err != nil {
			return nil, 0, nil, fmt.Errorf("extract %s: %w", f.Path, err)
		}
	}

	symNodes := make(map[grammar.Symbol]int64, len(syms))
	var fileNodeID int64
	if len(syms) > 0 {
		lang := ext.Language()
		for _, s := range syms {
			props, err := json.Marshal(map[string]any{
				"language":    lang,
				"kind":        s.Kind,
				"symbol_name": s.Name,
				"signature":   s.Signature,
				"byte_start":  s.ByteStart,
				"byte_end":    s.ByteEnd,
				"line_start":  s.LineStart,
				"line_end":    s.LineEnd,
			})
			if err != nil {
				return nil, 0, nil, fmt.Errorf("marshal symbol props: %w", err)
			}
			var nodeID int64
			if err := tx.QueryRowContext(ctx, `
INSERT INTO nodes(file_id, node_type, name, properties, provenance, created_at, updated_at)
VALUES (?, 'symbol', ?, ?, 'EXTRACTED', ?, ?)
RETURNING node_id`, f.ID, s.Name, string(props), now, now).Scan(&nodeID); err != nil {
				return nil, 0, nil, fmt.Errorf("insert symbol node %q: %w", s.Name, err)
			}
			symNodes[s] = nodeID
		}
	} else {
		props, err := json.Marshal(map[string]string{"mime": f.Mime})
		if err != nil {
			return nil, 0, nil, fmt.Errorf("marshal file props: %w", err)
		}
		if err := tx.QueryRowContext(ctx, `
INSERT INTO nodes(file_id, node_type, name, properties, provenance, created_at, updated_at)
VALUES (?, 'file', ?, ?, 'EXTRACTED', ?, ?)
RETURNING node_id`, f.ID, filepath.Base(f.Path), string(props), now, now).Scan(&fileNodeID); err != nil {
			return nil, 0, nil, fmt.Errorf("insert file node: %w", err)
		}
	}

	if err := markStatusTx(ctx, tx, f.ID, PassStructural, StatusDone, ""); err != nil {
		return nil, 0, nil, err
	}
	return symNodes, fileNodeID, syms, nil
}

// runChunker chunks the file (AST-per-symbol for code with symbols,
// recursive prose otherwise) and inserts the chunk rows. The chunks_ai
// trigger from migration 0001 syncs chunks_fts in the same transaction.
func (r *Runner) runChunker(ctx context.Context, tx *sql.Tx, f fileRow, content []byte, symNodes map[grammar.Symbol]int64, fileNodeID int64, syms []grammar.Symbol) ([]pendingChunk, error) {
	if err := markStatusTx(ctx, tx, f.ID, PassChunker, StatusRunning, ""); err != nil {
		return nil, err
	}

	var out []pendingChunk
	insert := func(nodeID int64, text string, byteStart, byteEnd, tokenCount int, kind string, header any) error {
		var chunkID int64
		if err := tx.QueryRowContext(ctx, `
INSERT INTO chunks(node_id, text, byte_start, byte_end, token_count, chunk_kind, header)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING chunk_id`, nodeID, text, byteStart, byteEnd, tokenCount, kind, header).Scan(&chunkID); err != nil {
			return fmt.Errorf("insert chunk: %w", err)
		}
		out = append(out, pendingChunk{chunkID: chunkID, text: text})
		return nil
	}

	if len(syms) > 0 {
		for _, c := range chunker.ChunkCode(content, syms, r.Cfg, r.Counter) {
			// ChunkCode emits the exact symbol slice for in-budget
			// symbols; oversize symbols fall through to the prose
			// chunker, producing strict sub-ranges of the symbol.
			kind := "code_ast"
			if c.ByteStart != int(c.Symbol.ByteStart) || c.ByteEnd != int(c.Symbol.ByteEnd) {
				kind = "code_fallback"
			}
			nodeID, ok := symNodes[c.Symbol]
			if !ok {
				return nil, fmt.Errorf("chunk for unknown symbol %q", c.Symbol.Name)
			}
			if err := insert(nodeID, c.Text, c.ByteStart, c.ByteEnd, c.TokenCount, kind, nil); err != nil {
				return nil, err
			}
		}
	} else {
		// Prose documents, code without a supported grammar, and code
		// with zero extracted symbols all chunk as prose (§4.2.6).
		kind := "prose"
		if f.ContentClass == "code" {
			kind = "code_fallback"
		}
		for _, c := range chunker.ChunkProse(string(content), r.Cfg, r.Counter) {
			var header any // NULL unless the chunker captured a heading
			if c.Header != "" {
				header = c.Header
			}
			if err := insert(fileNodeID, c.Text, c.ByteStart, c.ByteEnd, c.TokenCount, kind, header); err != nil {
				return nil, err
			}
		}
	}

	if err := markStatusTx(ctx, tx, f.ID, PassChunker, StatusDone, ""); err != nil {
		return nil, err
	}
	return out, nil
}

// flushEmbedBatch embeds every buffered chunk and writes the vectors
// through the fingerprint-guarded Writer inside the file's transaction.
func (r *Runner) flushEmbedBatch(tx *sql.Tx) error {
	if len(r.embedBuf) == 0 {
		return nil
	}
	pairs := make([]embed.EmbedPair, 0, len(r.embedBuf))
	for _, c := range r.embedBuf {
		vec, err := r.Embedder.EmbedQuery(c.text)
		if err != nil {
			return fmt.Errorf("embed chunk %d: %w", c.chunkID, err)
		}
		pairs = append(pairs, embed.EmbedPair{ChunkID: c.chunkID, Vector: bytesToInt8(vec)})
	}
	if err := r.Writer.WriteBatch(tx, pairs); err != nil {
		return fmt.Errorf("write embed batch: %w", err)
	}
	r.embedBuf = r.embedBuf[:0]
	return nil
}

// isBinaryDocument reports whether a 'document'-class file is a binary
// container format the M1 pipeline cannot extract text from. The list
// mirrors the binary subset of the walker's document classifier
// (walker/mime.go): .pdf/.docx/.odt are binary; .md/.txt/.rst/.org/.tex
// are plain text and chunk as prose.
func isBinaryDocument(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdf", ".docx", ".odt":
		return true
	}
	return false
}

// extractorFor selects the grammar for a code file by extension (§4.2.6).
// nil means "no supported grammar" — the file falls through to prose
// chunking with chunk_kind='code_fallback'.
func (r *Runner) extractorFor(f fileRow) grammar.Extractor {
	if f.ContentClass != "code" {
		return nil
	}
	switch strings.ToLower(filepath.Ext(f.Path)) {
	case ".go":
		return r.Extractors["go"]
	case ".py":
		return r.Extractors["python"]
	default:
		return nil
	}
}

func (r *Runner) loadFileRow(ctx context.Context, id int64) (fileRow, error) {
	var f fileRow
	err := r.DB.QueryRowContext(ctx,
		`SELECT file_id, path, size, mime, content_class FROM files WHERE file_id = ?`, id,
	).Scan(&f.ID, &f.Path, &f.Size, &f.Mime, &f.ContentClass)
	return f, err
}

// recordFailure upserts a failed pipeline_state row OUTSIDE the per-file
// transaction (which has already rolled back by the time a failure is
// recorded — a plain MarkFailed UPDATE would hit zero rows and the
// failure would be invisible, pre-mortem Story 2).
func (r *Runner) recordFailure(ctx context.Context, fileID int64, pass PassName, ferr error) {
	r.failedFiles++
	if pass == "" {
		pass = PassStructural
	}
	if _, err := r.DB.ExecContext(ctx, `
INSERT INTO pipeline_state(file_id, pass_name, status, checkpoint_at, error_message)
VALUES (?, ?, 'failed', ?, ?)
ON CONFLICT(file_id, pass_name) DO UPDATE SET
    status='failed', checkpoint_at=excluded.checkpoint_at, error_message=excluded.error_message`,
		fileID, string(pass), time.Now().Unix(), ferr.Error()); err != nil {
		r.logger().Error("runner: record failure", "file_id", fileID, "pass", string(pass), "error", err)
	}
	r.logger().Warn("runner: file failed", "file_id", fileID, "pass", string(pass), "error", ferr.Error())
}

// logSummary emits the end-of-Run failure histogram and summary line
// (D28b.1 step 6, pre-mortem Story 2): top-3 error prefixes at WARN
// (always, when failures exist), then a summary at INFO / WARN (>1%
// failure rate) / ERROR (>10%).
func (r *Runner) logSummary(ctx context.Context, total int) {
	logger := r.logger()
	rows, err := r.DB.QueryContext(ctx, `
SELECT substr(error_message, 1, 80) AS prefix, COUNT(*) AS n
FROM pipeline_state WHERE status='failed'
GROUP BY prefix ORDER BY n DESC LIMIT 3`)
	if err != nil {
		logger.Error("runner: failure histogram query", "error", err)
	} else {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var prefix string
			var n int
			if err := rows.Scan(&prefix, &n); err != nil {
				logger.Error("runner: failure histogram scan", "error", err)
				break
			}
			logger.Warn("runner: top failure", "error_prefix", prefix, "count", n)
		}
		if err := rows.Err(); err != nil {
			logger.Error("runner: failure histogram rows", "error", err)
		}
	}

	rate := 0.0
	if total > 0 {
		rate = float64(r.failedFiles) / float64(total)
	}
	args := []any{"files_total", total, "files_failed", r.failedFiles, "failure_rate", rate}
	switch {
	case rate > 0.10:
		logger.Error("runner: run complete with high failure rate", args...)
	case rate > 0.01:
		logger.Warn("runner: run complete with failures", args...)
	default:
		logger.Info("runner: run complete", args...)
	}
}

// markStatusTx is the tx-scoped equivalent of the MarkRunning/MarkDone
// helpers in pipeline.go. Those take *sql.DB, but store.Open pins the
// pool to a single connection, so a db-level Exec while the per-file
// transaction holds that connection would deadlock — every write inside
// runPerFile must go through the transaction.
func markStatusTx(ctx context.Context, tx *sql.Tx, fileID int64, pass PassName, status Status, errMsg string) error {
	var msg any
	if errMsg != "" {
		msg = errMsg
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO pipeline_state(file_id, pass_name, status, checkpoint_at, error_message)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(file_id, pass_name) DO UPDATE SET
    status=excluded.status, checkpoint_at=excluded.checkpoint_at, error_message=excluded.error_message`,
		fileID, string(pass), string(status), time.Now().Unix(), msg)
	return err
}

// readFileContent reads the file content with a size cap (§4.2.3).
// Oversize is a per-file failure, not fatal.
func readFileContent(path string, size int64) ([]byte, error) {
	if size > maxReadBytes {
		return nil, fmt.Errorf("oversize: %d bytes > cap %d", size, maxReadBytes)
	}
	return os.ReadFile(path)
}

// bytesToInt8 reinterprets the 384-byte quantised vector from
// Embedder.EmbedQuery as the []int8 that embed.Writer.WriteBatch expects
// (§4.2.2 — the inverse of the conversion inside writer.go).
func bytesToInt8(b []byte) []int8 {
	out := make([]int8, len(b))
	for i, v := range b {
		out[i] = int8(v) // Go spec: conversion reinterprets the byte as signed
	}
	return out
}

func (r *Runner) batchSize() int {
	if r.BatchSize > 0 {
		return r.BatchSize
	}
	return defaultEmbedBatch
}

func (r *Runner) logger() *slog.Logger {
	if r.Logger != nil {
		return r.Logger
	}
	return slog.Default()
}
