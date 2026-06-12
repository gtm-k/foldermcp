//go:build cgo

package pipeline

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gtm-k/foldermcp/internal/v3/chunker"
	"github.com/gtm-k/foldermcp/internal/v3/embed"
	"github.com/gtm-k/foldermcp/internal/v3/grammar"
	"github.com/gtm-k/foldermcp/internal/v3/store"
	"github.com/gtm-k/foldermcp/internal/v3/walker"
)

// --- test doubles -----------------------------------------------------

// stubEmbedder satisfies the package-local embedder interface (§4.2.1):
// deterministic 384-byte vectors, injectable Init failures, and a
// per-call hook — no ONNX Runtime in unit tests.
type stubEmbedder struct {
	initErr error
	calls   int
	onEmbed func(call int)
}

func (s *stubEmbedder) Init() error { return s.initErr }

func (s *stubEmbedder) EmbedQuery(text string) ([]byte, error) {
	s.calls++
	if s.onEmbed != nil {
		s.onEmbed(s.calls)
	}
	vec := make([]byte, embed.Dimension)
	for i := range vec {
		vec[i] = byte((i*7 + len(text)) % 256)
	}
	return vec, nil
}

// panicExtractor simulates a tree-sitter grammar edge-case panic
// (pre-mortem Story 2's failure class).
type panicExtractor struct{ lang string }

func (p panicExtractor) Language() string { return p.lang }
func (p panicExtractor) Extract([]byte) ([]grammar.Symbol, error) {
	panic("synthetic grammar panic for test")
}

// wc is the same word-count stub the chunker tests use.
type wc struct{}

func (wc) Count(s string) int {
	if strings.TrimSpace(s) == "" {
		return 0
	}
	return len(strings.Fields(s))
}

// --- fixtures ---------------------------------------------------------

const goFixture = `package fixture

// Hello returns a greeting for the runner happy-path test.
func Hello() string {
	return "hello from the go fixture"
}

// Add adds two integers together.
func Add(a, b int) int {
	return a + b
}
`

const pyFixture = `def greet(name):
    return "hello " + name


class Greeter:
    def greet(self):
        return "hi there from the python fixture"
`

const mdFixture = `# Fixture Document

This document describes connection pooling with configurable timeouts.

A second paragraph adds more prose so the chunker has material to work with.
`

func seedDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
	}
	return dir
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(store.Options{Path: filepath.Join(t.TempDir(), "runner.db"), Tier: store.TierMid})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(db, ""); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db
}

func newTestRunner(t *testing.T, db *sql.DB) *Runner {
	t.Helper()
	goEx, err := grammar.NewGoExtractor()
	if err != nil {
		t.Fatalf("NewGoExtractor: %v", err)
	}
	pyEx, err := grammar.NewPythonExtractor()
	if err != nil {
		t.Fatalf("NewPythonExtractor: %v", err)
	}
	return &Runner{
		DB:         db,
		Embedder:   &stubEmbedder{},
		Writer:     embed.NewWriter(db),
		Extractors: map[string]grammar.Extractor{"go": goEx, "python": pyEx},
		Counter:    wc{},
		Cfg:        chunker.DefaultConfig(),
	}
}

func count(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}

func fileIDByPathSuffix(t *testing.T, db *sql.DB, suffix string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(`SELECT file_id FROM files WHERE path LIKE ?`, "%"+suffix).Scan(&id); err != nil {
		t.Fatalf("file id for %s: %v", suffix, err)
	}
	return id
}

// --- tests (D28b.7 matrix) ---------------------------------------------

// TestRunner_HappyPath: 3-file fixture (1 go, 1 py, 1 md) → all three
// reach PassEmbeddings done with non-empty chunks and embeddings.
func TestRunner_HappyPath(t *testing.T) {
	db := openTestDB(t)
	dir := seedDir(t, map[string]string{
		"main.go": goFixture,
		"util.py": pyFixture,
		"doc.md":  mdFixture,
	})
	r := newTestRunner(t, db)

	if err := r.Run(context.Background(), dir); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := count(t, db, `SELECT COUNT(*) FROM files WHERE deleted_at IS NULL`); got != 3 {
		t.Errorf("files = %d, want 3", got)
	}
	// Every per-pass MarkDone is visible in pipeline_state (D28b.2).
	for _, pass := range []string{"structural", "chunker", "embeddings"} {
		if got := count(t, db, `SELECT COUNT(*) FROM pipeline_state WHERE pass_name=? AND status='done'`, pass); got != 3 {
			t.Errorf("%s done = %d, want 3", pass, got)
		}
	}
	nodes := count(t, db, `SELECT COUNT(*) FROM nodes`)
	if nodes == 0 {
		t.Error("no nodes written")
	}
	chunks := count(t, db, `SELECT COUNT(*) FROM chunks`)
	if chunks == 0 {
		t.Error("no chunks written")
	}
	embeddings := count(t, db, `SELECT COUNT(*) FROM embeddings`)
	if embeddings != chunks {
		t.Errorf("embeddings = %d, chunks = %d — want equal", embeddings, chunks)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM chunks WHERE chunk_kind='code_ast'`); got == 0 {
		t.Error("no code_ast chunks from go/py fixtures")
	}
	if got := count(t, db, `SELECT COUNT(*) FROM chunks WHERE chunk_kind='prose'`); got == 0 {
		t.Error("no prose chunks from md fixture")
	}
	// Symbol nodes carry extracted metadata (generated columns).
	if got := count(t, db, `SELECT COUNT(*) FROM nodes WHERE node_type='symbol' AND symbol_name='Hello' AND language='go'`); got != 1 {
		t.Errorf("Hello symbol node = %d, want 1", got)
	}
	// FTS followed the chunk inserts in the same txn.
	if got := count(t, db, `SELECT COUNT(*) FROM chunks_fts WHERE chunks_fts MATCH 'pooling'`); got == 0 {
		t.Error("chunks_fts has no hit for fixture prose")
	}
}

// TestRunner_IdempotentOnRetry (D8 Layer 2, §4.1): partial state from a
// simulated crashed prior run is wiped by the Runner's defensive delete —
// exactly one set of nodes/chunks/embeddings exists afterwards. The
// sha256 does NOT change here, so the Layer 1 trigger never fires; this
// isolates the Layer 2 (Runner) invariant.
func TestRunner_IdempotentOnRetry(t *testing.T) {
	db := openTestDB(t)
	dir := seedDir(t, map[string]string{"doc.md": mdFixture})
	ctx := context.Background()

	// Populate files via a real walk, then fake a crashed prior run.
	if _, err := walker.Walk(ctx, db, walker.Options{Root: dir}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	fileID := fileIDByPathSuffix(t, db, "doc.md")
	if _, err := db.Exec(`INSERT INTO nodes(file_id,node_type,name,provenance,created_at,updated_at) VALUES(?,'file','stale-node','EXTRACTED',1,1)`, fileID); err != nil {
		t.Fatal(err)
	}
	var staleChunk int64
	if err := db.QueryRow(`INSERT INTO chunks(node_id,text,byte_start,byte_end,token_count,chunk_kind) VALUES((SELECT node_id FROM nodes WHERE name='stale-node'),'stale leftover chunk',0,20,3,'prose') RETURNING chunk_id`).Scan(&staleChunk); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO embeddings(chunk_id, embedding) VALUES(?, vec_int8(?))`, staleChunk, make([]byte, 384)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO pipeline_state(file_id,pass_name,status,checkpoint_at) VALUES(?,'structural','running',1)`, fileID); err != nil {
		t.Fatal(err)
	}

	r := newTestRunner(t, db)
	if err := r.Run(ctx, dir); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := count(t, db, `SELECT COUNT(*) FROM nodes WHERE name='stale-node'`); got != 0 {
		t.Errorf("stale node survived: %d", got)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM chunks WHERE text LIKE '%stale leftover%'`); got != 0 {
		t.Errorf("stale chunk survived: %d", got)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM nodes WHERE file_id=?`, fileID); got != 1 {
		t.Errorf("nodes for file = %d, want exactly 1", got)
	}
	chunks := count(t, db, `SELECT COUNT(*) FROM chunks`)
	embeddings := count(t, db, `SELECT COUNT(*) FROM embeddings`)
	if chunks == 0 || embeddings != chunks {
		t.Errorf("chunks=%d embeddings=%d — want non-zero and equal", chunks, embeddings)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM pipeline_state WHERE file_id=? AND pass_name='embeddings' AND status='done'`, fileID); got != 1 {
		t.Errorf("embeddings done rows = %d, want 1", got)
	}
}

// TestRunner_PerFileFailureIsolation: a grammar panic on file 2 of 3 must
// not stop the run (scope fence): files 1 and 3 complete, file 2 records
// a structural failure with a non-empty error message, Run returns nil.
func TestRunner_PerFileFailureIsolation(t *testing.T) {
	db := openTestDB(t)
	dir := seedDir(t, map[string]string{
		"a.go": goFixture,
		"b.py": pyFixture,
		"c.md": mdFixture,
	})
	r := newTestRunner(t, db)
	r.Extractors["python"] = panicExtractor{lang: "python"}

	if err := r.Run(context.Background(), dir); err != nil {
		t.Fatalf("Run should isolate per-file failures, got: %v", err)
	}

	pyID := fileIDByPathSuffix(t, db, "b.py")
	var status, errMsg string
	if err := db.QueryRow(`SELECT status, error_message FROM pipeline_state WHERE file_id=? AND pass_name='structural'`, pyID).Scan(&status, &errMsg); err != nil {
		t.Fatalf("read failed row: %v", err)
	}
	if status != "failed" {
		t.Errorf("b.py structural status = %q, want failed", status)
	}
	if !strings.Contains(errMsg, "panic") || errMsg == "" {
		t.Errorf("b.py error_message = %q, want non-empty panic message", errMsg)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM pipeline_state WHERE pass_name='embeddings' AND status='done'`); got != 2 {
		t.Errorf("completed files = %d, want 2", got)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM nodes WHERE file_id=?`, pyID); got != 0 {
		t.Errorf("failed file left %d nodes — tx should have rolled back", got)
	}
}

// TestRunner_EmbedderInitErrorIsFatal (pre-mortem Story 1): a bad
// model/runtime path must abort the run before the file loop — Run
// returns the init error and no pipeline_state rows are written.
func TestRunner_EmbedderInitErrorIsFatal(t *testing.T) {
	db := openTestDB(t)
	dir := seedDir(t, map[string]string{"doc.md": mdFixture})
	sentinel := errors.New("ort init: missing libonnxruntime.so")
	r := newTestRunner(t, db)
	r.Embedder = &stubEmbedder{initErr: sentinel}

	err := r.Run(context.Background(), dir)
	if !errors.Is(err, sentinel) {
		t.Fatalf("Run = %v, want wrapped sentinel init error", err)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM pipeline_state`); got != 0 {
		t.Errorf("pipeline_state rows = %d, want 0 (init must fail before the loop)", got)
	}
}

// TestRunner_PartialFailureVisibleInStatus (pre-mortem Story 2): 2 of 10
// files fail → pipeline_state carries 2 structural failures (the rows
// admin Status aggregates into pass_counts["structural_failed"] in E2),
// the top-error-prefix WARN histogram fires, and the 20% failure rate
// crosses the 10% threshold for an ERROR summary.
func TestRunner_PartialFailureVisibleInStatus(t *testing.T) {
	db := openTestDB(t)
	files := map[string]string{
		"bad1.py": pyFixture,
		"bad2.py": pyFixture,
	}
	for _, n := range []string{"d1.md", "d2.md", "d3.md", "d4.md", "d5.md", "d6.md", "d7.md", "d8.md"} {
		files[n] = mdFixture
	}
	dir := seedDir(t, files)

	var logBuf bytes.Buffer
	r := newTestRunner(t, db)
	r.Extractors["python"] = panicExtractor{lang: "python"}
	r.Logger = slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	if err := r.Run(context.Background(), dir); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := count(t, db, `SELECT COUNT(*) FROM pipeline_state WHERE pass_name='structural' AND status='failed'`); got != 2 {
		t.Errorf("structural failed = %d, want 2", got)
	}
	logs := logBuf.String()
	if !strings.Contains(logs, "runner: top failure") || !strings.Contains(logs, "panic") {
		t.Errorf("missing WARN top-failure histogram with error prefix in logs:\n%s", logs)
	}
	if !strings.Contains(logs, "level=ERROR") || !strings.Contains(logs, "run complete with high failure rate") {
		t.Errorf("missing ERROR summary for 20%% failure rate in logs:\n%s", logs)
	}
}

// TestRunner_ShutdownDrainsBatch (ctx correctness): cancellation after 5
// of 10 files → Run returns ctx.Err(); exactly the 5 committed files
// show PassEmbeddings done and none is left 'running' (per-file
// transactions make partial files invisible).
func TestRunner_ShutdownDrainsBatch(t *testing.T) {
	db := openTestDB(t)
	files := make(map[string]string, 10)
	for _, n := range []string{"f0", "f1", "f2", "f3", "f4", "f5", "f6", "f7", "f8", "f9"} {
		// One short paragraph → exactly one chunk → exactly one
		// EmbedQuery call per file, so call 6 is the sixth file.
		files[n+".md"] = "short fixture paragraph for file " + n + "\n"
	}
	dir := seedDir(t, files)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stub := &stubEmbedder{}
	stub.onEmbed = func(call int) {
		if call == 6 {
			cancel() // cancel mid-file-6: its tx must never commit
		}
	}
	r := newTestRunner(t, db)
	r.Embedder = stub

	err := r.Run(ctx, dir)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v, want context.Canceled", err)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM pipeline_state WHERE pass_name='embeddings' AND status='done'`); got != 5 {
		t.Errorf("embeddings done = %d, want 5", got)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM pipeline_state WHERE status='running'`); got != 0 {
		t.Errorf("running rows = %d, want 0", got)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM pipeline_state WHERE status='failed'`); got != 0 {
		t.Errorf("failed rows = %d, want 0 (cancellation is not a per-file failure)", got)
	}
}

// TestRunner_Sha256ChangeReprocesses (D8 Layer 1): a content change
// invalidates downstream state via the migration-0002 trigger during the
// walker re-run, and the Runner picks the file up again.
func TestRunner_Sha256ChangeReprocesses(t *testing.T) {
	db := openTestDB(t)
	dir := seedDir(t, map[string]string{"doc.md": mdFixture})
	ctx := context.Background()
	r := newTestRunner(t, db)

	if err := r.Run(ctx, dir); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	fileID := fileIDByPathSuffix(t, db, "doc.md")
	if got := count(t, db, `SELECT COUNT(*) FROM pipeline_state WHERE file_id=? AND pass_name='embeddings' AND status='done'`, fileID); got != 1 {
		t.Fatalf("precondition: file not done after first run")
	}

	// Modify content → new sha256. A bare walker re-run (no Runner)
	// must fire the trigger and clear the pass ledger.
	if err := os.WriteFile(filepath.Join(dir, "doc.md"), []byte("completely replaced body about lease renewal semantics\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := walker.Walk(ctx, db, walker.Options{Root: dir}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM pipeline_state WHERE file_id=?`, fileID); got != 0 {
		t.Errorf("pipeline_state rows after sha256 change = %d, want 0 (trigger)", got)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM nodes WHERE file_id=?`, fileID); got != 0 {
		t.Errorf("nodes after sha256 change = %d, want 0 (trigger)", got)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM embeddings`); got != 0 {
		t.Errorf("embeddings after sha256 change = %d, want 0 (trigger)", got)
	}

	// Runner picks the file up on the next call.
	pending, err := PendingFiles(ctx, db, PassEmbeddings, 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, id := range pending {
		if id == fileID {
			found = true
		}
	}
	if !found {
		t.Errorf("file %d not in PendingFiles after sha256 change", fileID)
	}
	if err := r.Run(ctx, dir); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM chunks WHERE text LIKE '%lease renewal%'`); got == 0 {
		t.Error("new content not chunked on reprocess")
	}
	if got := count(t, db, `SELECT COUNT(*) FROM chunks WHERE text LIKE '%connection pooling%'`); got != 0 {
		t.Error("old content chunks survived reprocess")
	}
}

// TestReprocessLeavesNoOrphanedEmbeddings (Amendment 2026-06-11 item 4):
// embeddings is a vec0 virtual table with no FK cascade — after a
// content change and reprocess, the total embeddings count must equal
// the new chunk count exactly: no orphans, no duplicates.
func TestReprocessLeavesNoOrphanedEmbeddings(t *testing.T) {
	db := openTestDB(t)
	// Both fixtures exceed DefaultConfig().MaxTokens (260) under the wc
	// counter → multiple chunks per file, so orphan and duplicate states
	// are distinguishable from the happy path. (A custom small-budget
	// Config is no longer possible here: the Runner's policy drift guard
	// rejects any Cfg whose PolicyString differs from the fingerprint's.)
	long1 := strings.Repeat("alpha beta gamma delta epsilon zeta. ", 100) // 600 words
	long2 := strings.Repeat("omicron pi rho sigma kappa. ", 90)          // 450 words
	dir := seedDir(t, map[string]string{"multi.md": long1})
	ctx := context.Background()

	r := newTestRunner(t, db)

	if err := r.Run(ctx, dir); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	chunks1 := count(t, db, `SELECT COUNT(*) FROM chunks`)
	embeddings1 := count(t, db, `SELECT COUNT(*) FROM embeddings`)
	if chunks1 < 2 {
		t.Fatalf("fixture produced %d chunks, want ≥2", chunks1)
	}
	if embeddings1 != chunks1 {
		t.Fatalf("first run: embeddings=%d chunks=%d", embeddings1, chunks1)
	}

	// Modify content (different chunk count) and reprocess.
	if err := os.WriteFile(filepath.Join(dir, "multi.md"), []byte(long2), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.Run(ctx, dir); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	chunks2 := count(t, db, `SELECT COUNT(*) FROM chunks`)
	embeddings2 := count(t, db, `SELECT COUNT(*) FROM embeddings`)
	if chunks2 == 0 || embeddings2 != chunks2 {
		t.Errorf("after reprocess: embeddings=%d chunks=%d — want equal and non-zero (no orphans, no duplicates)", embeddings2, chunks2)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM embeddings e LEFT JOIN chunks c ON e.chunk_id = c.chunk_id WHERE c.chunk_id IS NULL`); got != 0 {
		t.Errorf("%d orphaned embedding rows point at dead chunks", got)
	}
}

// TestRunner_PolicyDriftGuard (D28b-E2 follow-up C, pre-mortem Story 3):
// the Writer's fingerprint records chunker.DefaultConfig().PolicyString();
// chunking under any other policy would persist chunks the fingerprint
// misdescribes. Run must fail fast before touching the DB.
func TestRunner_PolicyDriftGuard(t *testing.T) {
	db := openTestDB(t)
	dir := seedDir(t, map[string]string{"doc.md": mdFixture})
	r := newTestRunner(t, db)
	r.Cfg = chunker.Config{TargetTokens: 10, MinTokens: 3, MaxTokens: 15}

	err := r.Run(context.Background(), dir)
	if err == nil || !strings.Contains(err.Error(), "chunking policy") {
		t.Fatalf("Run = %v, want chunking-policy drift error", err)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM chunks`); got != 0 {
		t.Errorf("%d chunks written under a drifted policy — guard must fire before any work", got)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM files`); got != 0 {
		t.Errorf("%d file rows written — guard must fire before the walk", got)
	}
}

// TestRunner_BinaryDocumentSkipped (D28b-E2 follow-up D): content_class
// 'document' covers binary formats (.pdf/.docx/.odt) that M1 cannot
// extract text from — routing their raw bytes through the prose chunker
// produces garbage chunks. They must be marked skipped with the
// 'binary_document_pending_m2' marker (M2 Phase 4 replaces this with
// real PDF extraction); plain-text documents (.md/.txt/...) still chunk.
func TestRunner_BinaryDocumentSkipped(t *testing.T) {
	db := openTestDB(t)
	dir := seedDir(t, map[string]string{
		"report.pdf": "%PDF-1.4\n\xe2\xe3\xcf\xd3 binary garbage bytes that must never reach the prose chunker",
		"notes.md":   mdFixture,
	})
	r := newTestRunner(t, db)

	if err := r.Run(context.Background(), dir); err != nil {
		t.Fatalf("Run: %v", err)
	}

	pdfID := fileIDByPathSuffix(t, db, "report.pdf")
	for _, pass := range []string{"structural", "chunker", "embeddings"} {
		var status, msg string
		if err := db.QueryRow(`SELECT status, COALESCE(error_message,'') FROM pipeline_state WHERE file_id=? AND pass_name=?`, pdfID, pass).Scan(&status, &msg); err != nil {
			t.Fatalf("read %s row: %v", pass, err)
		}
		if status != "skipped" || msg != "binary_document_pending_m2" {
			t.Errorf("%s = (%q, %q), want (skipped, binary_document_pending_m2)", pass, status, msg)
		}
	}
	if got := count(t, db, `SELECT COUNT(*) FROM chunks c JOIN nodes n ON c.node_id = n.node_id WHERE n.file_id=?`, pdfID); got != 0 {
		t.Errorf("binary document produced %d garbage chunks", got)
	}
	// The plain-text document still completes the full pipeline.
	mdID := fileIDByPathSuffix(t, db, "notes.md")
	if got := count(t, db, `SELECT COUNT(*) FROM pipeline_state WHERE file_id=? AND pass_name='embeddings' AND status='done'`, mdID); got != 1 {
		t.Errorf("notes.md embeddings done = %d, want 1", got)
	}
}
