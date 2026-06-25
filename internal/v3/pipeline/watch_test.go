//go:build cgo

package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gtm-k/foldermcp/internal/watcher"
)

// fakeWatcher is a test event source satisfying watcherIface. It lets a test
// push events on demand and (critically) keep its channel artificially full +
// its consumer stalled to exercise the drop/reconcile path deterministically,
// without the production poller's wall-clock timing.
type fakeWatcher struct {
	ch       chan watcher.Event
	failures int
}

func newFakeWatcher(buf int) *fakeWatcher {
	return &fakeWatcher{ch: make(chan watcher.Event, buf)}
}
func (f *fakeWatcher) Start() <-chan watcher.Event { return f.ch }
func (f *fakeWatcher) Stop()                       { close(f.ch) }
func (f *fakeWatcher) ConsecutiveFailures() int    { return f.failures }

// newWatchLoopWithSource builds a WatchLoop wired to a caller-supplied
// watcherIface so tests skip the real filesystem poller.
func newWatchLoopWithSource(t *testing.T, w *WatchLoop, src watcherIface) {
	t.Helper()
	w.newWatcher = func() watcherIface { return src }
}

// orphanEmbeddings counts embeddings whose chunk_id no longer maps to a chunk —
// the dangling-vector invariant D13 must keep at zero.
func orphanEmbeddings(t *testing.T, w *WatchLoop) int {
	return count(t, w.db, `SELECT COUNT(*) FROM embeddings WHERE chunk_id NOT IN (SELECT chunk_id FROM chunks)`)
}

// --- GATING TEST 1: the drain goroutine never blocks on a full channel ------

// TestWatch_DrainNeverBlocksOnFullChannel asserts that the accumulate path
// (the drain goroutine's body) is decoupled from any pipeline work: feeding far
// more events than the channel buffer holds, with NO pipeline run in between,
// still lands every event in the dedup set. This is the structural guarantee
// that the watcher's emit() never silently drops because our consumer was busy.
func TestWatch_DrainNeverBlocksOnFullChannel(t *testing.T) {
	db := openTestDB(t)
	w := NewWatchLoop(db, newTestRunner(t, db), t.TempDir(), WatchConfig{}, nil, nil)

	// Drive accumulate directly (it is exactly the drain goroutine's per-event
	// body) far beyond the 64-slot channel size; it must never block.
	const n = 5000
	done := make(chan struct{})
	go func() {
		for i := 0; i < n; i++ {
			w.accumulate(watcher.Event{Path: filepath.Join("f", string(rune('a'+i%26)), itoa(i)), Type: "modified"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("accumulate blocked — drain would stall and the watcher would drop events")
	}
	if got := w.pendingCount(); got != n {
		t.Fatalf("pending = %d, want %d (every event accumulated)", got, n)
	}
}

// --- GATING TEST 2: dedup set coalesces duplicate events --------------------

func TestWatch_DedupCoalescesDuplicates(t *testing.T) {
	db := openTestDB(t)
	w := NewWatchLoop(db, newTestRunner(t, db), t.TempDir(), WatchConfig{}, nil, nil)

	for i := 0; i < 100; i++ {
		w.accumulate(watcher.Event{Path: "/x/same.go", Type: "modified"})
	}
	w.accumulate(watcher.Event{Path: "/x/other.go", Type: "created"})
	// Last-writer-wins: a final delete on same.go must collapse the 100 modifies.
	w.accumulate(watcher.Event{Path: "/x/same.go", Type: "deleted"})

	if got := w.pendingCount(); got != 2 {
		t.Fatalf("pending = %d, want 2 (duplicates coalesced)", got)
	}
	batch, _ := w.drainPending(1000)
	if batch["/x/same.go"] != "deleted" {
		t.Errorf("same.go = %q, want deleted (last writer wins)", batch["/x/same.go"])
	}
	if batch["/x/other.go"] != "created" {
		t.Errorf("other.go = %q, want created", batch["/x/other.go"])
	}
}

// --- GATING TEST 3: mtime-only change does NOT re-embed (D9 hash short-circuit)

// TestWatch_MtimeOnlyChangeDoesNotMarkPending indexes a file, captures its
// embeddings, then rewrites the SAME bytes (new mtime, identical sha256) and
// re-runs upsertCandidate. The sha256-change trigger must not fire, the
// pipeline_state must stay 'done', and the embeddings count must be unchanged.
func TestWatch_MtimeOnlyChangeDoesNotMarkPending(t *testing.T) {
	db := openTestDB(t)
	dir := seedDir(t, map[string]string{"doc.md": mdFixture})
	r := newTestRunner(t, db)
	w := NewWatchLoop(db, r, dir, WatchConfig{}, nil, nil)
	ctx := context.Background()

	if err := r.Run(ctx, dir); err != nil {
		t.Fatalf("initial Run: %v", err)
	}
	embBefore := count(t, db, `SELECT COUNT(*) FROM embeddings`)
	if embBefore == 0 {
		t.Fatal("no embeddings after initial index")
	}
	doneBefore := count(t, db, `SELECT COUNT(*) FROM pipeline_state WHERE pass_name='embeddings' AND status='done'`)

	// Rewrite identical content with a newer mtime (mtime-only churn).
	p := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(p, []byte(mdFixture), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	future := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(p, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	changed, err := w.upsertCandidate(ctx, p)
	if err != nil {
		t.Fatalf("upsertCandidate: %v", err)
	}
	if changed {
		t.Error("upsertCandidate reported a content change for mtime-only churn — sha256 must match")
	}
	// The 0002 trigger fires only on sha256 difference, so embeddings + done
	// rows are untouched and PendingFiles returns nothing.
	if got := count(t, db, `SELECT COUNT(*) FROM embeddings`); got != embBefore {
		t.Errorf("embeddings = %d, want %d (mtime-only churn must not re-embed)", got, embBefore)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM pipeline_state WHERE pass_name='embeddings' AND status='done'`); got != doneBefore {
		t.Errorf("embeddings-done = %d, want %d (state must stay done)", got, doneBefore)
	}
	pend, err := PendingFiles(ctx, db, PassEmbeddings, 100)
	if err != nil {
		t.Fatalf("PendingFiles: %v", err)
	}
	if len(pend) != 0 {
		t.Errorf("PendingFiles = %d, want 0 after mtime-only churn", len(pend))
	}
}

// TestWatch_ContentChangeMarksPending is the positive control: a TRUE content
// change must flip the file back to pending (trigger fires, downstream rows
// cleared) so the next Runner pass re-embeds it.
func TestWatch_ContentChangeMarksPending(t *testing.T) {
	db := openTestDB(t)
	dir := seedDir(t, map[string]string{"doc.md": mdFixture})
	r := newTestRunner(t, db)
	w := NewWatchLoop(db, r, dir, WatchConfig{}, nil, nil)
	ctx := context.Background()
	if err := r.Run(ctx, dir); err != nil {
		t.Fatalf("initial Run: %v", err)
	}

	p := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(p, []byte(mdFixture+"\n\nA brand new paragraph with different content entirely.\n"), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	changed, err := w.upsertCandidate(ctx, p)
	if err != nil {
		t.Fatalf("upsertCandidate: %v", err)
	}
	if !changed {
		t.Fatal("upsertCandidate did not report a content change for a real edit")
	}
	pend, err := PendingFiles(ctx, db, PassEmbeddings, 100)
	if err != nil {
		t.Fatalf("PendingFiles: %v", err)
	}
	if len(pend) != 1 {
		t.Fatalf("PendingFiles = %d, want 1 after a real content change", len(pend))
	}
	// And zero orphaned embeddings: the trigger deleted the old vectors.
	if got := orphanEmbeddings(t, w); got != 0 {
		t.Errorf("orphaned embeddings = %d, want 0 after invalidation", got)
	}
}

// --- GATING TEST 4: explicit delete chain removes embeddings (zero orphans) --

func TestWatch_DeleteChainRemovesEmbeddings(t *testing.T) {
	db := openTestDB(t)
	dir := seedDir(t, map[string]string{"a.md": mdFixture, "b.go": goFixture})
	r := newTestRunner(t, db)
	w := NewWatchLoop(db, r, dir, WatchConfig{}, nil, nil)
	ctx := context.Background()
	if err := r.Run(ctx, dir); err != nil {
		t.Fatalf("Run: %v", err)
	}

	embAll := count(t, db, `SELECT COUNT(*) FROM embeddings`)
	if embAll == 0 {
		t.Fatal("no embeddings to delete")
	}
	aPath := filepath.Join(dir, "a.md")
	aID := fileIDByPathSuffix(t, db, "a.md")
	embA := count(t, db, `SELECT COUNT(*) FROM embeddings WHERE chunk_id IN (
  SELECT chunk_id FROM chunks WHERE node_id IN (SELECT node_id FROM nodes WHERE file_id=?))`, aID)
	if embA == 0 {
		t.Fatal("a.md had no embeddings")
	}

	if err := w.deleteFile(ctx, aPath); err != nil {
		t.Fatalf("deleteFile: %v", err)
	}

	// File row and all downstream rows gone.
	if got := count(t, db, `SELECT COUNT(*) FROM files WHERE path=?`, aPath); got != 0 {
		t.Errorf("files row for a.md = %d, want 0", got)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM nodes WHERE file_id=?`, aID); got != 0 {
		t.Errorf("nodes for a.md = %d, want 0", got)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM pipeline_state WHERE file_id=?`, aID); got != 0 {
		t.Errorf("pipeline_state for a.md = %d, want 0", got)
	}
	// Zero ORPHANED embeddings: the explicit vec0 delete ran before nodes were
	// dropped, so a.md's vectors are gone, not dangling.
	if got := orphanEmbeddings(t, w); got != 0 {
		t.Errorf("orphaned embeddings = %d, want 0 (D13 vec0-first delete)", got)
	}
	// b.go's embeddings survive untouched.
	if got := count(t, db, `SELECT COUNT(*) FROM embeddings`); got != embAll-embA {
		t.Errorf("embeddings = %d, want %d (only a.md removed)", got, embAll-embA)
	}
}

// --- GATING TEST 5: reconciliation recovers a dropped event ------------------

// TestWatch_ReconciliationRecoversDroppedEvent simulates the v0.1 watcher
// dropping an event: a file is created on disk and its files row inserted
// (as a real walk would), but NO live event reaches the loop (the channel was
// full / consumer stalled). The periodic reconcile() must re-walk, index it,
// and bump events_reconciled_total.
func TestWatch_ReconciliationRecoversDroppedEvent(t *testing.T) {
	db := openTestDB(t)
	dir := seedDir(t, map[string]string{"seen.md": mdFixture})
	r := newTestRunner(t, db)
	w := NewWatchLoop(db, r, dir, WatchConfig{}, nil, nil)
	ctx := context.Background()

	// Initial index of the one known file.
	if err := r.Run(ctx, dir); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := w.Metrics().EventsReconciledTotal(); got != 0 {
		t.Fatalf("events_reconciled_total = %d before any drop, want 0", got)
	}

	// A new file appears on disk but its event is "dropped" — we never call
	// accumulate for it. Backdate its mtime so the overflow watermark is set.
	dropped := filepath.Join(dir, "dropped.md")
	if err := os.WriteFile(dropped, []byte("# Dropped\n\nThis file's create event was dropped by the full channel.\n"), 0o644); err != nil {
		t.Fatalf("write dropped: %v", err)
	}
	past := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(dropped, past, past); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	// The dedup set is empty (event was dropped), so a debounce batch would do
	// nothing. Only reconciliation recovers it.
	if got := w.pendingCount(); got != 0 {
		t.Fatalf("pending = %d, want 0 (event was dropped)", got)
	}
	if err := w.reconcile(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if got := count(t, db, `SELECT COUNT(*) FROM files WHERE path=?`, dropped); got != 1 {
		t.Fatalf("dropped.md not walked: files row = %d", got)
	}
	di := fileIDByPathSuffix(t, db, "dropped.md")
	if got := count(t, db, `SELECT COUNT(*) FROM pipeline_state WHERE file_id=? AND pass_name='embeddings' AND status='done'`, di); got != 1 {
		t.Fatalf("dropped.md not indexed by reconciliation: done = %d", got)
	}
	if got := w.Metrics().EventsReconciledTotal(); got < 1 {
		t.Errorf("events_reconciled_total = %d, want >=1 (dropped event recovered)", got)
	}
	if got := w.Metrics().OverflowRecoveryAgeUnix(); got == 0 {
		t.Error("overflow recovery watermark not set — dropped-event staleness is invisible")
	}
}

// --- GATING TEST 6: startup sweep purges rows for a removed file -------------

// TestWatch_StartupSweepPurgesRemovedFile indexes two files, then marks one
// deleted_at (exactly what Runner.Run's deletion reconciliation does for a file
// gone from disk — it sets deleted_at but leaves nodes/chunks/embeddings, the
// D19 gap). The startup sweep must purge all of that file's downstream rows
// with zero orphaned embeddings.
func TestWatch_StartupSweepPurgesRemovedFile(t *testing.T) {
	db := openTestDB(t)
	dir := seedDir(t, map[string]string{"keep.md": mdFixture, "gone.go": goFixture})
	r := newTestRunner(t, db)
	w := NewWatchLoop(db, r, dir, WatchConfig{}, nil, nil)
	ctx := context.Background()
	if err := r.Run(ctx, dir); err != nil {
		t.Fatalf("Run: %v", err)
	}

	goneID := fileIDByPathSuffix(t, db, "gone.go")
	embGone := count(t, db, `SELECT COUNT(*) FROM embeddings WHERE chunk_id IN (
  SELECT chunk_id FROM chunks WHERE node_id IN (SELECT node_id FROM nodes WHERE file_id=?))`, goneID)
	if embGone == 0 {
		t.Fatal("gone.go had no embeddings to purge")
	}
	// Simulate the Runner's deletion reconciliation: mark deleted_at only.
	if _, err := db.ExecContext(ctx, `UPDATE files SET deleted_at=? WHERE file_id=?`, time.Now().Unix(), goneID); err != nil {
		t.Fatalf("mark deleted: %v", err)
	}

	if err := w.startupSweep(ctx); err != nil {
		t.Fatalf("startupSweep: %v", err)
	}

	if got := count(t, db, `SELECT COUNT(*) FROM files WHERE file_id=?`, goneID); got != 0 {
		t.Errorf("gone.go files row = %d, want 0 (purged)", got)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM nodes WHERE file_id=?`, goneID); got != 0 {
		t.Errorf("gone.go nodes = %d, want 0", got)
	}
	if got := orphanEmbeddings(t, w); got != 0 {
		t.Errorf("orphaned embeddings = %d, want 0 after sweep", got)
	}
	// keep.md is untouched.
	if got := count(t, db, `SELECT COUNT(*) FROM files WHERE path LIKE '%keep.md'`); got != 1 {
		t.Errorf("keep.md missing after sweep: %d", got)
	}
}

// --- GATING TEST 7: end-to-end batch via the fake watcher --------------------

// TestWatch_ProcessBatchEndToEnd drives one debounce batch through the public
// path: events accumulate, processBatch upserts + runs the pipeline, and the
// edited file ends up indexed. Exercises the create/modify branch wiring that
// the unit tests above hit piecewise.
func TestWatch_ProcessBatchEndToEnd(t *testing.T) {
	db := openTestDB(t)
	dir := seedDir(t, map[string]string{"orig.md": mdFixture})
	r := newTestRunner(t, db)
	w := NewWatchLoop(db, r, dir, WatchConfig{}, nil, nil)
	src := newFakeWatcher(64)
	newWatchLoopWithSource(t, w, src)
	ctx := context.Background()

	if err := r.Run(ctx, dir); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// A new file appears; its create event is delivered.
	added := filepath.Join(dir, "added.md")
	if err := os.WriteFile(added, []byte("# Added\n\nFresh content visible after one batch.\n"), 0o644); err != nil {
		t.Fatalf("write added: %v", err)
	}
	w.accumulate(watcher.Event{Path: added, Type: "created"})

	if err := w.processBatch(ctx); err != nil {
		t.Fatalf("processBatch: %v", err)
	}
	ai := fileIDByPathSuffix(t, db, "added.md")
	if got := count(t, db, `SELECT COUNT(*) FROM pipeline_state WHERE file_id=? AND pass_name='embeddings' AND status='done'`, ai); got != 1 {
		t.Errorf("added.md not indexed by batch: done = %d", got)
	}
	if got := w.pendingCount(); got != 0 {
		t.Errorf("pending = %d after batch, want 0", got)
	}
}

// --- FIX A: the watcher must NOT surface walker-excluded paths ---------------

// TestWatch_UpsertSkipsWalkerExcludedPaths asserts that upsertCandidate refuses
// to insert a files row for any path the walker would never create — paths under
// .git/, node_modules/, etc., and dotfiles — so the watch path cannot diverge
// from the walker's write path (phantom rows + garbage embeddings). A normal file
// in the same batch must still be upserted.
func TestWatch_UpsertSkipsWalkerExcludedPaths(t *testing.T) {
	db := openTestDB(t)
	dir := t.TempDir()
	r := newTestRunner(t, db)
	w := NewWatchLoop(db, r, dir, WatchConfig{}, nil, nil)
	ctx := context.Background()

	// Excluded: an ignore-dir child, a nested ignore-dir child, and a dotfile.
	excluded := map[string]string{
		".git/config":               "[core]\n",
		"node_modules/pkg/index.js": "module.exports = {}\n",
		"sub/__pycache__/m.cpython": "bytecode\n",
		".hidden.md":                "# secret\n",
	}
	// Allowed: a normal file (and .env, which the walker's skip rule lets through).
	allowed := map[string]string{
		"doc.md": mdFixture,
		".env":   "KEY=value\n",
	}
	for rel, content := range excluded {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
		ok, err := w.upsertCandidate(ctx, p)
		if err != nil {
			t.Fatalf("upsertCandidate(%s) returned error: %v", rel, err)
		}
		if ok {
			t.Errorf("upsertCandidate(%s) reported a change for a walker-excluded path", rel)
		}
		if got := count(t, db, `SELECT COUNT(*) FROM files WHERE path=?`, p); got != 0 {
			t.Errorf("files row created for excluded path %s (count=%d) — diverges from walker", rel, got)
		}
	}
	for rel, content := range allowed {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
		if _, err := w.upsertCandidate(ctx, p); err != nil {
			t.Fatalf("upsertCandidate(%s) returned error: %v", rel, err)
		}
		if got := count(t, db, `SELECT COUNT(*) FROM files WHERE path=?`, p); got != 1 {
			t.Errorf("files row NOT created for allowed path %s (count=%d)", rel, got)
		}
	}
}

// TestWatch_WatcherBuiltWithWalkerExcludes asserts the underlying watcher is
// constructed with the walker's ignore set forwarded as excludes (belt #1), so
// the poller does not even emit events for those trees.
func TestWatch_WatcherBuiltWithWalkerExcludes(t *testing.T) {
	dir := t.TempDir()
	w := NewWatchLoop(openTestDB(t), newTestRunner(t, openTestDB(t)), dir, WatchConfig{}, nil, nil)
	got := w.cfg.Excludes
	// Every ignore-dir name must appear (top-level + nested forms via compileGlobs).
	for _, name := range []string{".git", "node_modules", "__pycache__", "target", "dist", "build", ".venv", "venv", ".foldermcp"} {
		found := false
		for _, g := range got {
			if g == name+"/**" || g == "**/"+name+"/**" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("watcher excludes missing ignore-dir %q; have %v", name, got)
		}
	}
}

// --- FIX B: reconcile must HARD-PURGE soft-deleted files ---------------------

// TestWatch_ReconcileHardPurgesDroppedDelete simulates a DROPPED delete event:
// an indexed file is removed from disk but no delete event reaches the loop.
// reconcile() runs Runner.Run, which soft-deletes (sets deleted_at) but leaves
// nodes/chunks/embeddings. The in-scope half of the fix is that reconcile then
// hard-purges those downstream rows — bounding staleness to one reconcile
// interval — so after reconcile there are ZERO orphaned embeddings for the file.
func TestWatch_ReconcileHardPurgesDroppedDelete(t *testing.T) {
	db := openTestDB(t)
	dir := seedDir(t, map[string]string{"keep.md": mdFixture, "gone.go": goFixture})
	r := newTestRunner(t, db)
	w := NewWatchLoop(db, r, dir, WatchConfig{}, nil, nil)
	ctx := context.Background()
	if err := r.Run(ctx, dir); err != nil {
		t.Fatalf("Run: %v", err)
	}
	goneID := fileIDByPathSuffix(t, db, "gone.go")
	embGone := count(t, db, `SELECT COUNT(*) FROM embeddings WHERE chunk_id IN (
  SELECT chunk_id FROM chunks WHERE node_id IN (SELECT node_id FROM nodes WHERE file_id=?))`, goneID)
	if embGone == 0 {
		t.Fatal("gone.go had no embeddings")
	}

	// Drop the file from disk; NO delete event is accumulated (dropped event).
	if err := os.Remove(filepath.Join(dir, "gone.go")); err != nil {
		t.Fatalf("remove gone.go: %v", err)
	}
	// Backdate last_seen so the Runner's deletion reconciliation
	// (last_seen < startTime) deterministically flags gone.go deleted_at on the
	// next Run — the initial Run and this reconcile would otherwise share the
	// same wall-clock second (Runner uses unix-second granularity), masking the
	// soft-delete the hard-purge is meant to clean up.
	if _, err := db.ExecContext(ctx, `UPDATE files SET last_seen=last_seen-3600 WHERE file_id=?`, goneID); err != nil {
		t.Fatalf("backdate last_seen: %v", err)
	}

	if err := w.reconcile(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	// Precondition the fix relies on: Runner.Run soft-deleted gone.go.
	if got := count(t, db, `SELECT COUNT(*) FROM files WHERE file_id=? AND deleted_at IS NOT NULL`, goneID); got != 1 &&
		count(t, db, `SELECT COUNT(*) FROM files WHERE file_id=?`, goneID) != 0 {
		t.Fatalf("gone.go was neither soft-deleted nor purged (deleted-flag=%d) — Runner deletion reconciliation precondition not met", got)
	}

	// The file row may remain flagged deleted_at OR be gone, but its downstream
	// rows (nodes/chunks/embeddings) MUST be purged: zero orphaned embeddings and
	// no nodes for the file.
	if got := count(t, db, `SELECT COUNT(*) FROM nodes WHERE file_id=?`, goneID); got != 0 {
		t.Errorf("nodes for soft-deleted gone.go = %d, want 0 after reconcile hard-purge", got)
	}
	if got := orphanEmbeddings(t, w); got != 0 {
		t.Errorf("orphaned embeddings = %d, want 0 after reconcile hard-purge", got)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM embeddings WHERE chunk_id IN (
  SELECT chunk_id FROM chunks WHERE node_id IN (SELECT node_id FROM nodes WHERE file_id=?))`, goneID); got != 0 {
		t.Errorf("embeddings still mapped to gone.go = %d, want 0", got)
	}
	// keep.md still indexed.
	if got := count(t, db, `SELECT COUNT(*) FROM files WHERE path LIKE '%keep.md' AND deleted_at IS NULL`); got != 1 {
		t.Errorf("keep.md missing/deleted after reconcile: %d", got)
	}
}

// --- FIX C: reconcile recovery metric must not count normal live updates -----

// TestWatch_ReconcileDoesNotCountPendingLiveEvent delivers a NORMAL live event
// (its path is in the dedup set) and then fires reconcile BEFORE the debounce
// processes it. The reconcile's Runner.Run will index the file, but because the
// file had a pending live event it is NOT a dropped-event recovery, so
// events_reconciled_total must stay 0.
func TestWatch_ReconcileDoesNotCountPendingLiveEvent(t *testing.T) {
	db := openTestDB(t)
	dir := seedDir(t, map[string]string{"seen.md": mdFixture})
	r := newTestRunner(t, db)
	w := NewWatchLoop(db, r, dir, WatchConfig{}, nil, nil)
	ctx := context.Background()
	if err := r.Run(ctx, dir); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// A new file appears AND its live event is delivered normally (pending set).
	live := filepath.Join(dir, "live.md")
	if err := os.WriteFile(live, []byte("# Live\n\nA normally-delivered live event for reconcile de-pollution.\n"), 0o644); err != nil {
		t.Fatalf("write live: %v", err)
	}
	w.accumulate(watcher.Event{Path: live, Type: "created"})

	// reconcile fires before processBatch drains the pending event.
	if err := w.reconcile(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if got := w.Metrics().EventsReconciledTotal(); got != 0 {
		t.Errorf("events_reconciled_total = %d after a normally-delivered live event, want 0 (no false-positive recovery)", got)
	}
}

// --- concurrency smoke: drain + run loop under -race (no live model) ---------

// TestWatch_RunLoopShutsDownCleanly starts the full Run loop with a fake
// watcher and a stub embedder, feeds a burst, then cancels — asserting the
// drain goroutine and loop exit without deadlock or data race.
func TestWatch_RunLoopShutsDownCleanly(t *testing.T) {
	db := openTestDB(t)
	dir := seedDir(t, map[string]string{"a.md": mdFixture})
	r := newTestRunner(t, db)
	w := NewWatchLoop(db, r, dir, WatchConfig{Debounce: 20 * time.Millisecond, ReconcileInterval: 50 * time.Millisecond}, nil, nil)
	src := newFakeWatcher(64)
	newWatchLoopWithSource(t, w, src)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = w.Run(ctx)
	}()

	for i := 0; i < 50; i++ {
		src.ch <- watcher.Event{Path: filepath.Join(dir, "a.md"), Type: "modified"}
	}
	time.Sleep(150 * time.Millisecond)
	cancel()

	doneCh := make(chan struct{})
	go func() { wg.Wait(); close(doneCh) }()
	select {
	case <-doneCh:
	case <-time.After(5 * time.Second):
		t.Fatal("watch loop did not shut down within 5s after cancel")
	}
}

// itoa is a tiny local int->string to avoid pulling strconv into this file's
// otherwise dependency-light test surface.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	s := string(b[p:])
	if neg {
		return "-" + s
	}
	return s
}
