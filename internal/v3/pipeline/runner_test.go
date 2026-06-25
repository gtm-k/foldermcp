//go:build cgo

package pipeline

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"os/exec"
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

// writeFile writes raw bytes (e.g. a real PDF/docx/odt fixture) into dir under
// name — the binary counterpart to seedDir's string map.
func writeFile(t *testing.T, dir, name string, content []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", name, err)
	}
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
	// rejects any Cfg that differs from chunker.DefaultConfig().)
	long1 := strings.Repeat("alpha beta gamma delta epsilon zeta. ", 100) // 600 words
	long2 := strings.Repeat("omicron pi rho sigma kappa. ", 90)           // 450 words
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
// misdescribes. Run must fail fast before touching the DB. The guard
// compares the full Config struct (E2 review finding 2), so drift in
// fields PolicyString omits — OverlapToks, activated by M2 — is caught
// too; both drift shapes are exercised below.
func TestRunner_PolicyDriftGuard(t *testing.T) {
	overlapOnly := chunker.DefaultConfig()
	overlapOnly.OverlapToks = 0 // invisible to PolicyString — full-struct guard must still catch it
	for name, cfg := range map[string]chunker.Config{
		"token_budget_drift": {TargetTokens: 10, MinTokens: 3, MaxTokens: 15},
		"overlap_only_drift": overlapOnly,
	} {
		t.Run(name, func(t *testing.T) {
			db := openTestDB(t)
			dir := seedDir(t, map[string]string{"doc.md": mdFixture})
			r := newTestRunner(t, db)
			r.Cfg = cfg

			err := r.Run(context.Background(), dir)
			if err == nil || !strings.Contains(err.Error(), "chunking config") {
				t.Fatalf("Run = %v, want chunking-config drift error", err)
			}
			if got := count(t, db, `SELECT COUNT(*) FROM chunks`); got != 0 {
				t.Errorf("%d chunks written under a drifted policy — guard must fire before any work", got)
			}
			if got := count(t, db, `SELECT COUNT(*) FROM files`); got != 0 {
				t.Errorf("%d file rows written — guard must fire before the walk", got)
			}
		})
	}
}

// TestRunner_BinaryDocumentsExtracted (A4): content_class 'document' covers
// binary formats (.pdf/.docx/.odt) that the M2 extractors now turn into text.
// They must be EXTRACTED and chunked (pdf_text / office_text), NOT skipped.
//
// R2 (blocking): the .docx/.odt fixtures are REAL ZIP containers built via
// chunker.BuildDOCX/BuildODT, so they carry NUL bytes in their first 8 KB — the
// exact condition that previously reclassified a document to 'unknown'. This
// test proves they are still classified 'document', routed to extraction, and
// never reach the binary-skip path or the prose chunker.
func TestRunner_BinaryDocumentsExtracted(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext not on PATH; skipping (gate runs in WSL where it is installed)")
	}
	db := openTestDB(t)
	dir := t.TempDir()

	// Real multi-page PDF fixture (committed under testdata/v3/pdf).
	pdfBytes, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "v3", "pdf", "multi_page.pdf"))
	if err != nil {
		t.Fatalf("read pdf fixture: %v", err)
	}
	// F5 (R2 BLOCKING): the PDF fixture MUST carry a NUL within its first 8 KB the
	// way real PDFs do — otherwise walker.IsBinaryContent(sample)==false and the
	// "stays content_class=document" assertion below passes WHETHER OR NOT the
	// .pdf exemption exists (a vacuous test — the Q2-retro masking failure). Fail
	// loudly here if a future regen drops the NUL, so the PDF half of the
	// exemption is genuinely exercised.
	sniff := pdfBytes
	if len(sniff) > 8192 {
		sniff = sniff[:8192]
	}
	if !bytes.Contains(sniff, []byte{0x00}) {
		t.Fatalf("PDF fixture has no NUL in its first 8192 bytes — the binary-exemption assertion would be vacuous (F5/R2). Regenerate multi_page.pdf with a NUL-bearing binary comment.")
	}
	if !walker.IsBinaryContent(sniff) {
		t.Fatalf("walker.IsBinaryContent(pdf sample)==false — the .pdf exemption is not being exercised (F5/R2)")
	}
	writeFile(t, dir, "report.pdf", pdfBytes)

	// Real docx + odt fixtures (NUL-bearing ZIP containers, R2).
	docx, err := chunker.BuildDOCX([]string{
		"This is the office docx body for the runner extraction test.",
		"It mentions tungsten alloy as a unique probe phrase for retrieval.",
	})
	if err != nil {
		t.Fatalf("BuildDOCX: %v", err)
	}
	writeFile(t, dir, "memo.docx", docx)
	odt, err := chunker.BuildODT([]string{
		"This is the odt content body for the runner extraction test.",
		"It mentions cobalt isotope as a unique probe phrase for retrieval.",
	})
	if err != nil {
		t.Fatalf("BuildODT: %v", err)
	}
	writeFile(t, dir, "report.odt", odt)
	writeFile(t, dir, "notes.md", []byte(mdFixture))

	r := newTestRunner(t, db)
	if err := r.Run(context.Background(), dir); err != nil {
		t.Fatalf("Run: %v", err)
	}

	cases := []struct {
		name string
		kind string
	}{
		{"report.pdf", "pdf_text"},
		{"memo.docx", "office_text"},
		{"report.odt", "office_text"},
	}
	for _, tc := range cases {
		id := fileIDByPathSuffix(t, db, tc.name)
		// R2: the file is classified 'document' (not downgraded to unknown by
		// its NUL bytes), so all three passes complete done — never skipped.
		for _, pass := range []string{"structural", "chunker", "embeddings"} {
			var status, msg string
			if err := db.QueryRow(`SELECT status, COALESCE(error_message,'') FROM pipeline_state WHERE file_id=? AND pass_name=?`, id, pass).Scan(&status, &msg); err != nil {
				t.Fatalf("%s %s row: %v", tc.name, pass, err)
			}
			if status != "done" {
				t.Errorf("%s %s = (%q, %q), want done", tc.name, pass, status, msg)
			}
		}
		var cc string
		if err := db.QueryRow(`SELECT content_class FROM files WHERE file_id=?`, id).Scan(&cc); err != nil {
			t.Fatalf("%s content_class: %v", tc.name, err)
		}
		if cc != "document" {
			t.Errorf("%s content_class = %q, want document (R2 — NUL bytes must not reclassify)", tc.name, cc)
		}
		chunks := count(t, db, `SELECT COUNT(*) FROM chunks c JOIN nodes n ON c.node_id=n.node_id WHERE n.file_id=? AND c.chunk_kind=?`, id, tc.kind)
		if chunks == 0 {
			t.Errorf("%s produced no %s chunks", tc.name, tc.kind)
		}
		embeddings := count(t, db, `SELECT COUNT(*) FROM embeddings e JOIN chunks c ON e.chunk_id=c.chunk_id JOIN nodes n ON c.node_id=n.node_id WHERE n.file_id=?`, id)
		if embeddings != chunks {
			t.Errorf("%s embeddings=%d chunks=%d, want equal", tc.name, embeddings, chunks)
		}
	}

	// FTS hit on a probe phrase that only exists inside an extracted office doc.
	if got := count(t, db, `SELECT COUNT(*) FROM chunks_fts WHERE chunks_fts MATCH 'tungsten'`); got == 0 {
		t.Error("no FTS hit for docx-only probe phrase 'tungsten' — extraction did not index")
	}

	// The plain-text document still completes the full pipeline as prose.
	mdID := fileIDByPathSuffix(t, db, "notes.md")
	if got := count(t, db, `SELECT COUNT(*) FROM pipeline_state WHERE file_id=? AND pass_name='embeddings' AND status='done'`, mdID); got != 1 {
		t.Errorf("notes.md embeddings done = %d, want 1", got)
	}
}

// TestRunner_PDFMissingDependency (A4 AC): with pdftotext unresolvable, a PDF is
// marked failed with error_message='dependency:pdftotext' and the indexer still
// exits 0 (per-file failure, not fatal).
func TestRunner_PDFMissingDependency(t *testing.T) {
	db := openTestDB(t)
	dir := t.TempDir()
	pdfBytes, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "v3", "pdf", "multi_page.pdf"))
	if err != nil {
		t.Fatalf("read pdf fixture: %v", err)
	}
	writeFile(t, dir, "report.pdf", pdfBytes)

	r := newTestRunner(t, db)
	// Force pdftotext resolution to fail regardless of the host PATH.
	r.PDFCfg = chunker.PDFConfig{PdftotextPath: filepath.Join(t.TempDir(), "no_such_pdftotext")}

	if err := r.Run(context.Background(), dir); err != nil {
		t.Fatalf("Run returned fatal error (want exit 0 / nil): %v", err)
	}

	pdfID := fileIDByPathSuffix(t, db, "report.pdf")
	var status, msg string
	if err := db.QueryRow(`SELECT status, COALESCE(error_message,'') FROM pipeline_state WHERE file_id=? AND pass_name='chunker'`, pdfID).Scan(&status, &msg); err != nil {
		t.Fatalf("read chunker row: %v", err)
	}
	if status != "failed" || msg != "dependency:pdftotext" {
		t.Errorf("chunker = (%q, %q), want (failed, dependency:pdftotext)", status, msg)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM chunks c JOIN nodes n ON c.node_id=n.node_id WHERE n.file_id=?`, pdfID); got != 0 {
		t.Errorf("failed PDF produced %d chunks, want 0 (transaction rolled back)", got)
	}
}

// TestRunner_EmptyExtractionFailsObservably (F1): a binary-document extraction
// that yields ZERO chunks must land status=FAILED with error_message=
// "extraction_empty" — NOT done-with-zero-chunks (which would count the file
// indexed, drop it from PendingFiles forever, and leave it silently
// unsearchable). Two paths: a valid PDF whose pdftotext output is only a
// form-feed (empty_text.pdf), and a docx with no paragraphs (text-empty office).
func TestRunner_EmptyExtractionFailsObservably(t *testing.T) {
	db := openTestDB(t)
	dir := t.TempDir()

	// Empty-text PDF (pdftotext output = "\f"); needs pdftotext.
	havePdftotext := false
	if _, err := exec.LookPath("pdftotext"); err == nil {
		havePdftotext = true
		pdfBytes, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "v3", "pdf", "empty_text.pdf"))
		if err != nil {
			t.Fatalf("read empty pdf fixture: %v", err)
		}
		writeFile(t, dir, "blank.pdf", pdfBytes)
	}

	// Text-empty docx: no paragraphs → zero extracted chunks. No pdftotext needed.
	emptyDocx, err := chunker.BuildDOCX(nil)
	if err != nil {
		t.Fatalf("BuildDOCX(nil): %v", err)
	}
	writeFile(t, dir, "empty.docx", emptyDocx)

	r := newTestRunner(t, db)
	if err := r.Run(context.Background(), dir); err != nil {
		t.Fatalf("Run: %v", err)
	}

	check := func(name string) {
		id := fileIDByPathSuffix(t, db, name)
		var status, msg string
		if err := db.QueryRow(`SELECT status, COALESCE(error_message,'') FROM pipeline_state WHERE file_id=? AND pass_name='chunker'`, id).Scan(&status, &msg); err != nil {
			t.Fatalf("%s chunker row: %v", name, err)
		}
		if status != "failed" || msg != "extraction_empty" {
			t.Errorf("%s chunker = (%q, %q), want (failed, extraction_empty)", name, status, msg)
		}
		// The transaction rolled back: no node/chunk persisted, and the file is NOT
		// marked done (so it stays a pending retry, not silently indexed-empty).
		if got := count(t, db, `SELECT COUNT(*) FROM nodes WHERE file_id=?`, id); got != 0 {
			t.Errorf("%s left %d nodes — tx should have rolled back", name, got)
		}
		if got := count(t, db, `SELECT COUNT(*) FROM pipeline_state WHERE file_id=? AND pass_name='embeddings' AND status='done'`, id); got != 0 {
			t.Errorf("%s embeddings marked done despite zero chunks (silent index-empty)", name)
		}
	}
	if havePdftotext {
		check("blank.pdf")
	}
	check("empty.docx")
}

// TestRunner_DocumentOversizeFailsWithBareMarker (re-review FIX 1/5a): a binary
// document whose on-disk size exceeds maxDocumentBytes must land status=FAILED
// with error_message EXACTLY "document_oversize" (the bare sentinel) — no
// interpolated byte count — so the end-of-run histogram (which buckets on
// substr(error_message,1,80)) groups every oversize document into ONE bucket
// rather than one bucket per distinct size. A sparse file gives the walker a
// >cap stat size without writing 64 MB to disk; the .docx PK magic + extension
// route it to runExtractedDocument, where the size cap rejects it before any
// extractor opens it.
func TestRunner_DocumentOversizeFailsWithBareMarker(t *testing.T) {
	db := openTestDB(t)
	dir := t.TempDir()

	path := filepath.Join(dir, "huge.docx")
	fh, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Real ZIP local-file-header magic so content classification routes it as a
	// binary document (not skipped as unknown), then a sparse hole out to >cap.
	if _, err := fh.Write([]byte("PK\x03\x04")); err != nil {
		t.Fatalf("write magic: %v", err)
	}
	if err := fh.Truncate(maxDocumentBytes + 1); err != nil {
		t.Fatalf("truncate sparse: %v", err)
	}
	if err := fh.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	r := newTestRunner(t, db)
	if err := r.Run(context.Background(), dir); err != nil {
		t.Fatalf("Run returned fatal error (want exit 0 / nil): %v", err)
	}

	id := fileIDByPathSuffix(t, db, "huge.docx")
	var status, msg string
	if err := db.QueryRow(`SELECT status, COALESCE(error_message,'') FROM pipeline_state WHERE file_id=? AND pass_name='chunker'`, id).Scan(&status, &msg); err != nil {
		t.Fatalf("read chunker row: %v", err)
	}
	if status != "failed" || msg != "document_oversize" {
		t.Errorf("chunker = (%q, %q), want (failed, document_oversize) EXACTLY — bare marker, no byte count", status, msg)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM nodes WHERE file_id=?`, id); got != 0 {
		t.Errorf("oversize doc left %d nodes — must be rejected before any DB write", got)
	}
}

// TestRunner_NonIndexableSkipped (Q2 Gap B): the image/media/data/unknown skip
// branch (runner.go ~253) had no test — only the .pdf document branch did. This
// locks the behavior for every non-indexable class, including the literal PNG
// case: each file's three passes are marked skipped with the empty marker
// (distinct from 'binary_document_pending_m2' and 'binary_content_detected'),
// and none produces a node/chunk/embedding. Characterization test of existing
// correct behavior — expected to pass on first run.
func TestRunner_NonIndexableSkipped(t *testing.T) {
	db := openTestDB(t)
	dir := seedDir(t, map[string]string{
		"photo.png": "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR pretend image payload",
		"clip.mp3":  "ID3\x04\x00\x00\x00 pretend audio payload",
		// A5 reroutes csv/tsv/json/yaml/xml OUT of the skipped 'data' class into the
		// chunker, so a .csv is no longer a valid "skipped" representative. .sqlite
		// stays in the skipped 'data' class (binary DB container, not a text format),
		// so it is the correct stand-in for the data class here.
		"store.sqlite": "SQLite format 3\x00\x01\x02 pretend db page bytes",
		// renamed.txt exercises the walker's content-binary override end-to-end:
		// a text EXTENSION (classFromExtOrMime → 'document') whose early NUL makes
		// the walker downgrade it to 'unknown'. Without the override this file
		// would be prose-chunked; this is the only fixture here whose skip
		// depends on the Q2 walker change (blob_noext is already 'unknown' via
		// the octet-stream mime fallback, independent of the override).
		"renamed.txt": "looks like a text note\x00\x01\x02 but is actually binary",
		"blob_noext":  "\x00\x01\x02\x03 raw binary with no extension",
		"keep.md":     mdFixture,
	})
	r := newTestRunner(t, db)

	if err := r.Run(context.Background(), dir); err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, name := range []string{"photo.png", "clip.mp3", "store.sqlite", "renamed.txt", "blob_noext"} {
		id := fileIDByPathSuffix(t, db, name)
		for _, pass := range []string{"structural", "chunker", "embeddings"} {
			var status, msg string
			if err := db.QueryRow(`SELECT status, COALESCE(error_message,'') FROM pipeline_state WHERE file_id=? AND pass_name=?`, id, pass).Scan(&status, &msg); err != nil {
				t.Fatalf("%s %s row: %v", name, pass, err)
			}
			if status != "skipped" || msg != "" {
				t.Errorf("%s %s = (%q, %q), want (skipped, \"\")", name, pass, status, msg)
			}
		}
		if got := count(t, db, `SELECT COUNT(*) FROM nodes WHERE file_id=?`, id); got != 0 {
			t.Errorf("%s produced %d nodes, want 0", name, got)
		}
		if got := count(t, db, `SELECT COUNT(*) FROM chunks c JOIN nodes n ON c.node_id = n.node_id WHERE n.file_id=?`, id); got != 0 {
			t.Errorf("%s produced %d chunks, want 0", name, got)
		}
	}
	// The real document alongside them still indexes fully.
	mdID := fileIDByPathSuffix(t, db, "keep.md")
	if got := count(t, db, `SELECT COUNT(*) FROM pipeline_state WHERE file_id=? AND pass_name='embeddings' AND status='done'`, mdID); got != 1 {
		t.Errorf("keep.md embeddings done = %d, want 1", got)
	}
}

// TestRunner_BinaryContentSkipped (Q2 Gap A, route 2 + defense-in-depth): a file
// whose leading bytes are clean text — so the walker's 8 KB sample classifies it
// 'document' — but whose body then turns binary must be caught by the runner's
// full-buffer content check before chunking, not prose-chunked as garbage. The
// clean prefix here must exceed walker's binarySniffBytes (8192) so the file
// genuinely reaches the runner classified as a text document.
func TestRunner_BinaryContentSkipped(t *testing.T) {
	db := openTestDB(t)
	cleanPrefix := strings.Repeat("clean prose text. ", 1200) // ~21.6 KB, all valid UTF-8
	binaryTail := string([]byte{0x00, 0x01, 0x02, 0x00})
	dir := seedDir(t, map[string]string{
		"header-then-binary.md": cleanPrefix + binaryTail,
		"notes.md":              mdFixture,
	})
	r := newTestRunner(t, db)

	if err := r.Run(context.Background(), dir); err != nil {
		t.Fatalf("Run: %v", err)
	}

	binID := fileIDByPathSuffix(t, db, "header-then-binary.md")
	for _, pass := range []string{"structural", "chunker", "embeddings"} {
		var status, msg string
		if err := db.QueryRow(`SELECT status, COALESCE(error_message,'') FROM pipeline_state WHERE file_id=? AND pass_name=?`, binID, pass).Scan(&status, &msg); err != nil {
			t.Fatalf("read %s row: %v", pass, err)
		}
		if status != "skipped" || msg != "binary_content_detected" {
			t.Errorf("%s = (%q, %q), want (skipped, binary_content_detected)", pass, status, msg)
		}
	}
	if got := count(t, db, `SELECT COUNT(*) FROM chunks c JOIN nodes n ON c.node_id = n.node_id WHERE n.file_id=?`, binID); got != 0 {
		t.Errorf("binary-bodied file produced %d garbage chunks", got)
	}
	// The clean document alongside it still completes the full pipeline.
	mdID := fileIDByPathSuffix(t, db, "notes.md")
	if got := count(t, db, `SELECT COUNT(*) FROM pipeline_state WHERE file_id=? AND pass_name='embeddings' AND status='done'`, mdID); got != 1 {
		t.Errorf("notes.md embeddings done = %d, want 1", got)
	}
}

// TestRunner_StructuredDataIndexed (A5): a .csv and a .json file — formerly
// skipped as content_class 'data' — are now chunked + embedded with the right
// chunk_kinds (csv_schema/csv_rows for CSV, data_structured for JSON), reaching
// embeddings done. A .yaml and .xml round out the structure-aware formats.
func TestRunner_StructuredDataIndexed(t *testing.T) {
	db := openTestDB(t)
	var csvb strings.Builder
	csvb.WriteString("id,name,price\n")
	for i := 0; i < 50; i++ {
		csvb.WriteString("1,tungsten widget,9.99\n")
	}
	dir := seedDir(t, map[string]string{
		"products.csv":  csvb.String(),
		"config.json":   `{"service":"indexer","note":"cobalt isotope probe"}`,
		"settings.yaml": "service: indexer\nnote: molybdenum filament probe\n",
		"doc.xml":       "<config><note>vanadium oxide probe</note></config>",
		"notes.md":      mdFixture,
	})
	r := newTestRunner(t, db)
	if err := r.Run(context.Background(), dir); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// CSV → csv_schema + csv_rows, all passes done, embeddings == chunks.
	csvID := fileIDByPathSuffix(t, db, "products.csv")
	for _, pass := range []string{"structural", "chunker", "embeddings"} {
		var status, msg string
		if err := db.QueryRow(`SELECT status, COALESCE(error_message,'') FROM pipeline_state WHERE file_id=? AND pass_name=?`, csvID, pass).Scan(&status, &msg); err != nil {
			t.Fatalf("csv %s row: %v", pass, err)
		}
		if status != "done" {
			t.Errorf("csv %s = (%q,%q), want done", pass, status, msg)
		}
	}
	if got := count(t, db, `SELECT COUNT(*) FROM chunks c JOIN nodes n ON c.node_id=n.node_id WHERE n.file_id=? AND c.chunk_kind='csv_schema'`, csvID); got != 1 {
		t.Errorf("csv_schema chunks = %d, want 1", got)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM chunks c JOIN nodes n ON c.node_id=n.node_id WHERE n.file_id=? AND c.chunk_kind='csv_rows'`, csvID); got == 0 {
		t.Error("no csv_rows chunks")
	}
	csvChunks := count(t, db, `SELECT COUNT(*) FROM chunks c JOIN nodes n ON c.node_id=n.node_id WHERE n.file_id=?`, csvID)
	csvEmb := count(t, db, `SELECT COUNT(*) FROM embeddings e JOIN chunks c ON e.chunk_id=c.chunk_id JOIN nodes n ON c.node_id=n.node_id WHERE n.file_id=?`, csvID)
	if csvEmb != csvChunks {
		t.Errorf("csv embeddings=%d chunks=%d, want equal", csvEmb, csvChunks)
	}

	// JSON/YAML/XML → data_structured chunks.
	for _, suffix := range []string{"config.json", "settings.yaml", "doc.xml"} {
		id := fileIDByPathSuffix(t, db, suffix)
		var status string
		if err := db.QueryRow(`SELECT status FROM pipeline_state WHERE file_id=? AND pass_name='embeddings'`, id).Scan(&status); err != nil {
			t.Fatalf("%s embeddings row: %v", suffix, err)
		}
		if status != "done" {
			t.Errorf("%s embeddings status = %q, want done", suffix, status)
		}
		if got := count(t, db, `SELECT COUNT(*) FROM chunks c JOIN nodes n ON c.node_id=n.node_id WHERE n.file_id=? AND c.chunk_kind='data_structured'`, id); got == 0 {
			t.Errorf("%s produced no data_structured chunks", suffix)
		}
	}

	// FTS hits on probe phrases that only exist inside structured-data files.
	for _, probe := range []string{"cobalt", "molybdenum", "vanadium"} {
		if got := count(t, db, `SELECT COUNT(*) FROM chunks_fts WHERE chunks_fts MATCH ?`, probe); got == 0 {
			t.Errorf("no FTS hit for structured-data probe %q", probe)
		}
	}
}

// TestRunner_MalformedCSVFallsBackAmbiguous (A5 / D18): a ragged CSV falls back
// to prose chunking and its file NODE row carries provenance='AMBIGUOUS'
// (provenance is a nodes column, not chunks). The file still indexes — never
// crashes, never skipped.
func TestRunner_MalformedCSVFallsBackAmbiguous(t *testing.T) {
	db := openTestDB(t)
	dir := seedDir(t, map[string]string{
		// Ragged: column counts vary wildly → ChunkCSV returns ErrDataFallback.
		"ragged.csv": "a,b,c\n1,2\n3,4,5,6,7\nx\n,,\n9,10,11,12,13\n",
	})
	r := newTestRunner(t, db)
	if err := r.Run(context.Background(), dir); err != nil {
		t.Fatalf("Run: %v", err)
	}
	id := fileIDByPathSuffix(t, db, "ragged.csv")
	// Embeddings done (indexed as prose, not skipped/failed).
	var status string
	if err := db.QueryRow(`SELECT status FROM pipeline_state WHERE file_id=? AND pass_name='embeddings'`, id).Scan(&status); err != nil {
		t.Fatalf("embeddings row: %v", err)
	}
	if status != "done" {
		t.Errorf("ragged.csv embeddings = %q, want done (prose fallback)", status)
	}
	// The file node row is AMBIGUOUS (D18: provenance on the node, by query).
	if got := count(t, db, `SELECT COUNT(*) FROM nodes WHERE file_id=? AND provenance='AMBIGUOUS'`, id); got != 1 {
		t.Errorf("AMBIGUOUS file nodes = %d, want 1 (D18 malformed CSV)", got)
	}
	// Fallback produced prose chunks, NOT csv_schema/csv_rows.
	if got := count(t, db, `SELECT COUNT(*) FROM chunks c JOIN nodes n ON c.node_id=n.node_id WHERE n.file_id=? AND c.chunk_kind='prose'`, id); got == 0 {
		t.Error("no prose chunks from malformed-CSV fallback")
	}
	if got := count(t, db, `SELECT COUNT(*) FROM chunks c JOIN nodes n ON c.node_id=n.node_id WHERE n.file_id=? AND c.chunk_kind IN ('csv_schema','csv_rows')`, id); got != 0 {
		t.Errorf("malformed CSV minted %d csv_* chunks, want 0 (should fall back)", got)
	}
}

// TestRunner_BinaryJSONSkipped (A5 / R3 BLOCKING): a .json whose CONTENT is
// binary (NUL bytes) is skipped, never chunked — the classifier downgrades it to
// 'unknown' (NUL gate) so the runner's non-indexable skip drops it. Index by
// content, not by extension.
func TestRunner_BinaryJSONSkipped(t *testing.T) {
	db := openTestDB(t)
	dir := t.TempDir()
	writeFile(t, dir, "binary.json", []byte("{\x00\x01\x02 not really json at all}"))
	writeFile(t, dir, "good.json", []byte(`{"k":"real json value"}`))
	r := newTestRunner(t, db)
	if err := r.Run(context.Background(), dir); err != nil {
		t.Fatalf("Run: %v", err)
	}
	binID := fileIDByPathSuffix(t, db, "binary.json")
	for _, pass := range []string{"structural", "chunker", "embeddings"} {
		var status string
		if err := db.QueryRow(`SELECT status FROM pipeline_state WHERE file_id=? AND pass_name=?`, binID, pass).Scan(&status); err != nil {
			t.Fatalf("binary.json %s row: %v", pass, err)
		}
		if status != "skipped" {
			t.Errorf("binary.json %s = %q, want skipped (R3 NUL gate)", pass, status)
		}
	}
	if got := count(t, db, `SELECT COUNT(*) FROM chunks c JOIN nodes n ON c.node_id=n.node_id WHERE n.file_id=?`, binID); got != 0 {
		t.Errorf("binary .json produced %d chunks, want 0", got)
	}
	// The genuine JSON alongside it indexes.
	goodID := fileIDByPathSuffix(t, db, "good.json")
	if got := count(t, db, `SELECT COUNT(*) FROM chunks c JOIN nodes n ON c.node_id=n.node_id WHERE n.file_id=? AND c.chunk_kind='data_structured'`, goodID); got == 0 {
		t.Error("good.json produced no data_structured chunks")
	}
}
