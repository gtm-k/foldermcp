//go:build cgo

package pipeline

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gtm-k/foldermcp/internal/v3/walker"
	"github.com/gtm-k/foldermcp/internal/watcher"
)

// Default watch-loop timings (D14). All overridable via WatchConfig.
const (
	defaultWatchInterval     = 2 * time.Second  // underlying poll cadence
	defaultWatchDebounce     = 2 * time.Second  // coalesce a burst of edits
	defaultWatchReconcile    = 10 * time.Minute // full re-walk safety net (D14)
	defaultWatchBatchPerTick = 500              // bounded re-hash work per debounce tick
)

// WatchConfig configures the watch loop. Zero values fall back to the
// package defaults above.
type WatchConfig struct {
	// Interval is the underlying watcher poll cadence (how often the v0.1
	// poller stat-walks the tree). Defaults to 2s.
	Interval time.Duration
	// Debounce is how long the loop waits after the first pending event
	// before draining the dedup set and running a batch. Coalesces a burst
	// of edits to one pipeline run. Defaults to 2s.
	Debounce time.Duration
	// ReconcileInterval is the period of the full walker re-walk + PendingFiles
	// sweep that recovers events the watcher dropped at its 64-slot channel
	// (D14). Defaults to 10m.
	ReconcileInterval time.Duration
	// MaxBatchPerTick bounds the number of candidate paths re-hashed in one
	// debounce tick so a storm cannot make a single tick unbounded. Remaining
	// candidates stay in the dedup set for the next tick. Defaults to 500.
	MaxBatchPerTick int
	// Includes/Excludes are glob filters forwarded to the underlying watcher.
	Includes []string
	Excludes []string
}

func (c WatchConfig) interval() time.Duration {
	if c.Interval > 0 {
		return c.Interval
	}
	return defaultWatchInterval
}

func (c WatchConfig) debounce() time.Duration {
	if c.Debounce > 0 {
		return c.Debounce
	}
	return defaultWatchDebounce
}

func (c WatchConfig) reconcile() time.Duration {
	if c.ReconcileInterval > 0 {
		return c.ReconcileInterval
	}
	return defaultWatchReconcile
}

func (c WatchConfig) maxBatch() int {
	if c.MaxBatchPerTick > 0 {
		return c.MaxBatchPerTick
	}
	return defaultWatchBatchPerTick
}

// WatchMetrics holds the process-level counters surfaced in `index-v3 status`
// (D14 round-2 finding #3 + pre-mortem Story 3 observability). All fields are
// accessed via atomics so the gRPC admin handler can read them concurrently
// with the watch loop's writes. Pending-file count and oldest-pending age are
// DB-derived and live in status.go, not here.
type WatchMetrics struct {
	// EventsReconciledTotal counts files indexed because a reconciliation pass
	// found them pending — i.e. the watcher never delivered (or dropped) their
	// event. A nonzero value means the channel-drop safety net actually fired.
	eventsReconciledTotal atomic.Int64
	// overflowRecoveryAgeUnix is the unix mtime of the OLDEST change recovered
	// only by reconciliation (not by a live event). status.go converts this to
	// watch_overflow_recovery_age_seconds = now - this. 0 means "no overflow
	// recovery yet" (status reports age 0).
	overflowRecoveryAgeUnix atomic.Int64
}

// EventsReconciledTotal returns the running count of files first indexed by a
// reconciliation sweep rather than a live event.
func (m *WatchMetrics) EventsReconciledTotal() int64 { return m.eventsReconciledTotal.Load() }

// OverflowRecoveryAgeUnix returns the unix mtime of the oldest change recovered
// only by reconciliation, or 0 if none.
func (m *WatchMetrics) OverflowRecoveryAgeUnix() int64 { return m.overflowRecoveryAgeUnix.Load() }

// recordReconciledFiles bumps the reconciled-event counter and, if any of the
// recovered files predates the current overflow watermark, lowers the watermark
// to the oldest so watch_overflow_recovery_age_seconds reflects the worst-case
// staleness a dropped event suffered (D14 SLO boundary).
func (m *WatchMetrics) recordReconciledFiles(n int, oldestMtimeUnix int64) {
	if n <= 0 {
		return
	}
	m.eventsReconciledTotal.Add(int64(n))
	if oldestMtimeUnix <= 0 {
		return
	}
	for {
		cur := m.overflowRecoveryAgeUnix.Load()
		if cur != 0 && cur <= oldestMtimeUnix {
			return // already tracking an older (more stale) recovery
		}
		if m.overflowRecoveryAgeUnix.CompareAndSwap(cur, oldestMtimeUnix) {
			return
		}
	}
}

// watcherIface is the read-only subset of *watcher.Watcher the loop drives. It
// exists so watch_test.go can substitute a fake event source without a live
// filesystem poller (the production poller's timing is non-deterministic).
type watcherIface interface {
	Start() <-chan watcher.Event
	Stop()
	ConsecutiveFailures() int
}

// WatchLoop drives the existing pipeline.Runner over files as they change on
// disk. It owns NONE of the Runner's write path: every re-index goes through
// Runner.Run (D9 — events are candidates, not commands; the sha256 trigger
// short-circuits mtime-only churn), so the watch loop inherits the Runner's
// transactional per-file atomicity and the zero-chunk-done guards on the
// extracted-document and structured-data paths rather than forking a divergent
// write path.
//
// CAVEAT (FIX E): the Runner's PLAIN-TEXT path (runChunker → ChunkProse) does
// NOT guard zero chunks, so an emptied .go/.md re-indexed via watch is still
// marked done-with-zero-chunks. That guard lives in runner.go (off-limits this
// round) and is a tracked integration-pass follow-up — the invariant is systemic
// (flagged across A4/A5/A6), not specific to the watch loop.
type WatchLoop struct {
	db     *sql.DB
	runner *Runner
	root   string
	cfg    WatchConfig
	logger *slog.Logger
	metric *WatchMetrics

	// newWatcher builds the underlying event source. Overridable in tests.
	newWatcher func() watcherIface

	// pending is the unbounded dedup accumulator the drain goroutine fills
	// (D14). A path maps to its latest event type so a create-then-delete in
	// one window resolves to delete. Guarded by mu.
	mu      sync.Mutex
	pending map[string]string // path -> latest event type
}

// NewWatchLoop builds a watch loop. runner must be a fully-initialised Runner
// (same one index-v3/all-v3 would use for a one-shot run); metric may be nil,
// in which case the loop allocates a private one (status wiring then sees no
// counters, but the loop still works).
func NewWatchLoop(db *sql.DB, runner *Runner, root string, cfg WatchConfig, metric *WatchMetrics, logger *slog.Logger) *WatchLoop {
	if metric == nil {
		metric = &WatchMetrics{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	// FIX A (belt #1): forward the walker's ignore set to the watcher so the
	// poller does not even EMIT events for trees the walker hard-excludes
	// (.git/, node_modules/, build dirs, …). The authoritative guard is the
	// per-path skip check in upsertCandidate (belt #2) — the poller's glob
	// filter is best-effort because watcher.matchesFilter globs the full
	// relative path and cannot express the walker's "dotfile basename" rule.
	cfg.Excludes = mergeExcludes(cfg.Excludes, walkerIgnoreExcludeGlobs())
	w := &WatchLoop{
		db:      db,
		runner:  runner,
		root:    root,
		cfg:     cfg,
		logger:  logger,
		metric:  metric,
		pending: make(map[string]string),
	}
	w.newWatcher = func() watcherIface {
		return watcher.New(root, cfg.interval(), cfg.Includes, cfg.Excludes)
	}
	return w
}

// walkerIgnoreExcludeGlobs renders the walker's ignore-dir set (the SINGLE source
// of truth, walker.IgnoreDirNames) as watcher exclude globs. compileGlobs
// auto-derives the top-level form (name/**) from the "**/" prefix, so a single
// "**/<name>/**" pattern covers both nested and root-level occurrences. Dotfiles
// and secret-named files are intentionally NOT globbed here (the glob path cannot
// honour the per-basename rules); upsertCandidate's walker.ShouldSkip check does.
func walkerIgnoreExcludeGlobs() []string {
	names := walker.IgnoreDirNames()
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, "**/"+name+"/**")
	}
	return out
}

// mergeExcludes appends extra patterns not already present in base.
func mergeExcludes(base, extra []string) []string {
	seen := make(map[string]bool, len(base))
	for _, p := range base {
		seen[p] = true
	}
	out := base
	for _, p := range extra {
		if !seen[p] {
			out = append(out, p)
			seen[p] = true
		}
	}
	return out
}

// Metrics returns the loop's metric registry (the one passed in, or the private
// one allocated when nil was passed) so a caller can hand the same pointer to
// the gRPC admin handler.
func (w *WatchLoop) Metrics() *WatchMetrics { return w.metric }

// Run blocks until ctx is cancelled, draining watcher events into the dedup set
// and processing batches on the debounce cadence with periodic reconciliation.
// Backpressure: exactly one pipeline run executes at a time — events that
// arrive during a run accumulate in the dedup set for the next batch (no
// unbounded goroutines, D14).
func (w *WatchLoop) Run(ctx context.Context) error {
	// Startup sweep (D19): purge rows for files that vanished since the last
	// run BEFORE the first reconciliation, so a query in the first 10 minutes
	// never hits a deleted file. Best-effort: a sweep error is logged, not
	// fatal (the periodic reconciliation retries).
	if err := w.startupSweep(ctx); err != nil {
		w.logger.Warn("watch: startup sweep failed", "error", err)
	}

	// Startup reconciliation (D14): a full re-walk + PendingFiles sweep catches
	// everything that changed while the daemon was down.
	if err := w.reconcile(ctx); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		w.logger.Warn("watch: startup reconciliation failed", "error", err)
	}

	wch := w.newWatcher()
	events := wch.Start()
	defer wch.Stop()

	// Dedicated drain goroutine (D14): empties the 64-slot channel into the
	// unbounded dedup set the instant an event lands. It NEVER blocks on the
	// pipeline — that is what keeps the watcher's emit() from silently dropping
	// events because our consumer was busy re-indexing.
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-events:
				if !ok {
					return // watcher stopped
				}
				w.accumulate(ev)
			}
		}
	}()

	debounce := time.NewTicker(w.cfg.debounce())
	defer debounce.Stop()
	reconcileT := time.NewTicker(w.cfg.reconcile())
	defer reconcileT.Stop()

	for {
		select {
		case <-ctx.Done():
			<-drainDone
			return ctx.Err()
		case <-debounce.C:
			if err := w.processBatch(ctx); err != nil {
				if ctx.Err() != nil {
					<-drainDone
					return ctx.Err()
				}
				w.logger.Warn("watch: batch failed", "error", err)
			}
			// NAS-disconnect observability: surface the poller's degraded state
			// (it serves from cache and retries; we just make it visible).
			if f := wch.ConsecutiveFailures(); f >= 3 {
				w.logger.Warn("watch: source appears disconnected; serving cached index", "consecutive_failures", f)
			}
		case <-reconcileT.C:
			if err := w.reconcile(ctx); err != nil {
				if ctx.Err() != nil {
					<-drainDone
					return ctx.Err()
				}
				w.logger.Warn("watch: reconciliation failed", "error", err)
			}
		}
	}
}

// accumulate records one event in the dedup set. Last writer wins per path so a
// modify-then-delete collapses to delete (and a delete-then-recreate collapses
// to created/modified). Called only from the drain goroutine.
func (w *WatchLoop) accumulate(ev watcher.Event) {
	w.mu.Lock()
	w.pending[ev.Path] = ev.Type
	w.mu.Unlock()
}

// drainPending atomically removes and returns up to max entries from the dedup
// set; the remainder stays for the next tick (bounded batch per tick, D14
// backpressure). Returns the coalesced map and whether more remain.
func (w *WatchLoop) drainPending(max int) (map[string]string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pending) == 0 {
		return nil, false
	}
	out := make(map[string]string, len(w.pending))
	n := 0
	for path, typ := range w.pending {
		out[path] = typ
		delete(w.pending, path)
		n++
		if n >= max {
			break
		}
	}
	return out, len(w.pending) > 0
}

// pendingCount reports the dedup-set depth (test/observability helper).
func (w *WatchLoop) pendingCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.pending)
}

// processBatch drains one bounded batch of coalesced events, applies deletes
// explicitly, re-hashes upsert candidates (so the sha256 trigger fires only on
// true content change, D9), then runs ONE Runner pass over the pending set.
func (w *WatchLoop) processBatch(ctx context.Context) error {
	batch, _ := w.drainPending(w.cfg.maxBatch())
	if len(batch) == 0 {
		return nil
	}

	var deletes []string
	var upserts []string
	for path, typ := range batch {
		if typ == "deleted" {
			deletes = append(deletes, path)
		} else {
			upserts = append(upserts, path)
		}
	}

	for _, path := range deletes {
		if err := w.deleteFile(ctx, path); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			w.logger.Warn("watch: delete failed", "path", path, "error", err)
		}
	}

	changed := 0
	for _, path := range upserts {
		if err := ctx.Err(); err != nil {
			return err
		}
		ok, err := w.upsertCandidate(ctx, path)
		if err != nil {
			w.logger.Warn("watch: upsert candidate failed", "path", path, "error", err)
			continue
		}
		if ok {
			changed++
		}
	}

	// Backpressure (D14): one run at a time. Runner.Run re-walks root (idempotent
	// — unchanged files hash-short-circuit through the 0002 trigger) and processes
	// every pending file, so it also catches anything the single-file upserts
	// missed. Skip the run only when this batch produced no upsert candidates AND
	// no deletes — deletes already mutated the index above.
	if len(upserts) == 0 {
		return nil
	}
	return w.runner.Run(ctx, w.root)
}

// upsertCandidate re-hashes ONE file and upserts its files row with the same
// ON CONFLICT(path) DO UPDATE the walker uses, so the migration-0002
// sha256-change trigger fires (and only fires) on a TRUE content change (D9).
// mtime-only churn keeps the same sha256 → the trigger no-ops → the file's
// pipeline_state stays 'done' → PendingFiles skips it → no re-embed. Returns
// whether the row's content actually changed (sha256 differs from the stored
// row), which is what the storm test asserts.
func (w *WatchLoop) upsertCandidate(ctx context.Context, path string) (bool, error) {
	// FIX A (belt #2, authoritative) + CRITICAL watch-path secret deny: never
	// insert a files row the walker would not. This calls the SINGLE exported
	// walker predicate (walker.ShouldSkip) — the exact same rules Walk applies:
	// ignore-dirs + dotfile/IgnoreGlobs + the Layer-1 secretDenyGlobs deny
	// (.env, *.pem, *.key, id_rsa*, *credentials*, …). So the live watch path
	// cannot diverge from the one-shot walk and a secret-named file is rejected
	// BEFORE any hash/classify/stat work — never entering the index.
	//
	// includeSecrets=false (secure default): watch mode does not opt back into
	// indexing credential-bearing files. The watch loop carries no per-run
	// IgnoreGlobs source, so it passes nil (matches NewWatchLoop, which forwards
	// only the ignore-dir excludes to the poller).
	if walker.ShouldSkip(w.root, path, false, nil) {
		return false, nil
	}
	sum, err := walker.HashFile(path)
	if err != nil {
		// File vanished between event and re-hash, or unreadable. Treat a
		// vanished file as a delete so the index does not keep stale rows.
		return false, w.deleteFile(ctx, path)
	}
	mime, class, err := walker.ClassifyFile(path)
	if err != nil {
		return false, fmt.Errorf("classify %s: %w", path, err)
	}
	info, err := statFile(path)
	if err != nil {
		return false, w.deleteFile(ctx, path)
	}

	// Did the content change? Compare against the stored sha256 BEFORE the
	// upsert so the caller (and the storm test) can count true content changes.
	var prior []byte
	_ = w.db.QueryRowContext(ctx, `SELECT sha256 FROM files WHERE path = ?`, path).Scan(&prior)
	changed := !bytesEqual(prior, sum)

	now := time.Now().Unix()
	if _, err := w.db.ExecContext(ctx, `
INSERT INTO files(path, sha256, size, mtime, mime, content_class, parent_dir, last_seen)
VALUES (?,?,?,?,?,?,?,?)
ON CONFLICT(path) DO UPDATE SET
    sha256=excluded.sha256, size=excluded.size, mtime=excluded.mtime,
    mime=excluded.mime, content_class=excluded.content_class,
    last_seen=excluded.last_seen, deleted_at=NULL`,
		path, sum, info.size, info.mtime, mime, class, parentDir(path), now); err != nil {
		return false, fmt.Errorf("upsert %s: %w", path, err)
	}
	return changed, nil
}

// deleteFile purges every row for a file path. Order is load-bearing (D13):
// embeddings is a vec0 virtual table OUTSIDE SQLite's FK graph — nothing
// cascades to it — so its rows must be deleted by chunk_id WHILE the chunk rows
// that identify them still exist. nodes/chunks/chunks_fts/pipeline_state then
// cascade (or auto-sync via the chunks_ad FTS trigger) when the file row is
// deleted. Runs in one transaction so a query can never observe a half-deleted
// file.
func (w *WatchLoop) deleteFile(ctx context.Context, path string) error {
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var fileID int64
	if err := tx.QueryRowContext(ctx, `SELECT file_id FROM files WHERE path = ?`, path).Scan(&fileID); err != nil {
		// No such file (already gone): nothing to delete, not an error.
		if err == sql.ErrNoRows {
			committed = true
			return tx.Commit()
		}
		return err
	}

	if err := deleteFileRowsTx(ctx, tx, fileID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

// deleteFileRowsTx is the explicit D13 delete chain for one file_id, run inside
// tx. Shared by the watch delete path and the startup sweep so both honour the
// vec0-first ordering identically (single source of truth).
func deleteFileRowsTx(ctx context.Context, tx *sql.Tx, fileID int64) error {
	// 1. embeddings FIRST (vec0, no FK cascade — D13).
	if _, err := tx.ExecContext(ctx, `DELETE FROM embeddings WHERE chunk_id IN (
  SELECT chunk_id FROM chunks WHERE node_id IN (
    SELECT node_id FROM nodes WHERE file_id = ?))`, fileID); err != nil {
		return fmt.Errorf("delete embeddings: %w", err)
	}
	// 2. nodes — cascades to chunks (FK) → chunks_fts (chunks_ad trigger).
	if _, err := tx.ExecContext(ctx, `DELETE FROM nodes WHERE file_id = ?`, fileID); err != nil {
		return fmt.Errorf("delete nodes: %w", err)
	}
	// 3. pipeline_state for the file (cascades on files delete too, but be
	//    explicit so a future schema change cannot orphan it).
	if _, err := tx.ExecContext(ctx, `DELETE FROM pipeline_state WHERE file_id = ?`, fileID); err != nil {
		return fmt.Errorf("delete pipeline_state: %w", err)
	}
	// 4. the file row itself.
	if _, err := tx.ExecContext(ctx, `DELETE FROM files WHERE file_id = ?`, fileID); err != nil {
		return fmt.Errorf("delete file: %w", err)
	}
	return nil
}

// startupSweep purges downstream rows for files that were marked deleted_at by a
// prior Runner.Run deletion reconciliation but never had their nodes/chunks/
// embeddings purged (D19 — the Runner only sets deleted_at; full purge is
// watch-mode's job). Also covers files whose path no longer exists on disk.
// Runs each file's purge in its own transaction so one failure does not abort
// the whole sweep.
func (w *WatchLoop) startupSweep(ctx context.Context) error {
	return w.purgeSoftDeleted(ctx)
}

// purgeSoftDeleted hard-purges the downstream rows (nodes/chunks/embeddings/
// pipeline_state) of every file flagged deleted_at but not yet purged. Shared by
// startupSweep and reconcile (FIX B): Runner.Run only SOFT-deletes (sets
// deleted_at) for files gone from disk, leaving their vectors searchable until a
// purge runs. Calling this after EACH reconcile bounds that staleness window to
// one reconcile interval instead of one daemon restart. Each file's purge runs
// in its own transaction so one failure does not abort the sweep.
//
// NOTE (deferred, off-limits this round): the IMMEDIATE half — hydrateNodes
// honouring f.deleted_at so a soft-deleted file is excluded from query results
// the instant Runner.Run flags it (before this purge) — lives in
// grpc/query/hydrate.go and is a tracked integration-pass follow-up.
func (w *WatchLoop) purgeSoftDeleted(ctx context.Context) error {
	rows, err := w.db.QueryContext(ctx, `SELECT file_id FROM files WHERE deleted_at IS NOT NULL`)
	if err != nil {
		return fmt.Errorf("purge soft-deleted query: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()

	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := w.purgeFileID(ctx, id); err != nil {
			w.logger.Warn("watch: soft-deleted purge failed", "file_id", id, "error", err)
		}
	}
	return nil
}

// purgeFileID runs the D13 delete chain for one file_id in its own transaction.
func (w *WatchLoop) purgeFileID(ctx context.Context, fileID int64) error {
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if err := deleteFileRowsTx(ctx, tx, fileID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

// reconcile runs the full safety-net pass (D14): a Runner.Run, which re-walks
// root (recovering any file whose event the watcher dropped at its channel) and
// processes every pending file. Any file that reaches embeddings=done DURING
// this sweep but was NOT already done before it is, by definition, a file the
// live-event path never delivered — a dropped or never-delivered event. Those
// bump events_reconciled_total and lower the overflow-recovery watermark to the
// oldest such file's mtime so a dropped-event SLO violation is observable
// rather than silent.
func (w *WatchLoop) reconcile(ctx context.Context) error {
	// Snapshot the file_ids already at embeddings=done BEFORE the run. A walk
	// during Run may both DISCOVER a brand-new file (whose create event was
	// dropped — it is not yet in the files table) and process the existing
	// pending backlog; comparing the done-set delta captures both.
	preDone, err := w.doneEmbeddingIDs(ctx)
	if err != nil {
		w.logger.Warn("watch: pre-reconcile done-set snapshot failed", "error", err)
	}

	// FIX C: snapshot the paths that have a PENDING live event right now. A file
	// whose event WAS delivered (it sits in the dedup set) and is merely processed
	// by this reconcile's Run because the reconcile ticker beat the debounce
	// ticker is NOT a dropped-event recovery — counting it would raise a
	// false-positive SLO signal on normal operation. Such paths are excluded from
	// the recovered set below.
	livePending := w.pendingPaths()

	if err := w.runner.Run(ctx, w.root); err != nil {
		return err
	}

	recovered, oldestMtime := w.recoveredSince(ctx, preDone, livePending)
	w.metric.recordReconciledFiles(recovered, oldestMtime)

	// FIX B: Runner.Run only SOFT-deletes files gone from disk (sets deleted_at).
	// Hard-purge their downstream rows now so a dropped DELETE event's stale
	// vectors are bounded to one reconcile interval rather than one daemon
	// restart. Best-effort: a purge error is logged inside, not fatal.
	if err := w.purgeSoftDeleted(ctx); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		w.logger.Warn("watch: reconcile soft-deleted purge failed", "error", err)
	}
	return nil
}

// pendingPaths returns a snapshot of the paths currently in the dedup set
// (test/observability helper; used by reconcile to de-pollute the recovery
// metric — a path with a live event is not a dropped-event recovery).
func (w *WatchLoop) pendingPaths() map[string]bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make(map[string]bool, len(w.pending))
	for p := range w.pending {
		out[p] = true
	}
	return out
}

// doneEmbeddingIDs returns the set of file_ids currently at embeddings=done.
func (w *WatchLoop) doneEmbeddingIDs(ctx context.Context) (map[int64]bool, error) {
	rows, err := w.db.QueryContext(ctx, `
SELECT file_id FROM pipeline_state WHERE pass_name='embeddings' AND status='done'`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	set := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		set[id] = true
	}
	return set, rows.Err()
}

// recoveredSince counts file_ids now at embeddings=done that were NOT in the
// pre-run done set AND did NOT have a pending live event (livePending, by path),
// and returns the oldest mtime among them (for the overflow watermark). A file
// already done before the run is not a recovery; a file whose live event was
// delivered and merely processed by this sweep (FIX C) is not a recovery either.
func (w *WatchLoop) recoveredSince(ctx context.Context, preDone map[int64]bool, livePending map[string]bool) (int, int64) {
	rows, err := w.db.QueryContext(ctx, `
SELECT ps.file_id, f.mtime, f.path
FROM pipeline_state ps
JOIN files f ON f.file_id = ps.file_id
WHERE ps.pass_name='embeddings' AND ps.status='done' AND f.deleted_at IS NULL`)
	if err != nil {
		w.logger.Warn("watch: recovered-set query failed", "error", err)
		return 0, 0
	}
	defer func() { _ = rows.Close() }()
	n := 0
	var oldest int64
	for rows.Next() {
		var id, mtime int64
		var path string
		if err := rows.Scan(&id, &mtime, &path); err != nil {
			w.logger.Warn("watch: recovered-set scan failed", "error", err)
			return n, oldest
		}
		if preDone[id] {
			continue // already done before this sweep — not a recovery
		}
		if livePending[path] {
			continue // FIX C: live event was delivered — not a dropped-event recovery
		}
		n++
		if oldest == 0 || mtime < oldest {
			oldest = mtime
		}
	}
	return n, oldest
}

// --- small fs/byte helpers (kept local; watch.go is the only consumer) ---

type fileStat struct {
	size  int64
	mtime int64
}

func statFile(path string) (fileStat, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return fileStat{}, err
	}
	return fileStat{size: fi.Size(), mtime: fi.ModTime().Unix()}, nil
}

func parentDir(path string) string { return filepath.Dir(path) }

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
