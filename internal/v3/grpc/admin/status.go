//go:build cgo

// Package admin contains IndexAdmin RPC handlers (Status + Health in M1).
package admin

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
)

// WatchMetricsProvider exposes the watch loop's process-level counters to the
// Status handler without admin importing the pipeline package (avoids a
// dependency-direction coupling: pipeline.WatchMetrics satisfies this). nil
// when the daemon is not running in --watch mode.
type WatchMetricsProvider interface {
	EventsReconciledTotal() int64
	// OverflowRecoveryAgeUnix is the unix mtime of the oldest change recovered
	// only by reconciliation, or 0 if none. Status converts it to an age.
	OverflowRecoveryAgeUnix() int64
}

// Handler implements IndexAdmin.Status and IndexAdmin.Health.
type Handler struct {
	DB        *sql.DB
	StartTime time.Time
	// Watch is set only when the daemon runs in --watch mode; nil otherwise.
	// When set, Status surfaces events_reconciled_total and
	// watch_overflow_recovery_age_seconds.
	Watch WatchMetricsProvider
}

func (h *Handler) Status(ctx context.Context, _ *pb.StatusRequest) (*pb.StatusResponse, error) {
	var total int64
	_ = h.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM files WHERE deleted_at IS NULL`).Scan(&total)

	var indexed int64
	_ = h.DB.QueryRowContext(ctx, `
SELECT COUNT(DISTINCT file_id) FROM pipeline_state
WHERE pass_name='embeddings' AND status='done'`).Scan(&indexed)

	// Both pass-count queries log at WARN on failure instead of silently
	// dropping counts from the payload (E2 review finding 3,
	// actor-observability): a DB error here means per-pass progress and
	// failure rates vanish from Status — exactly the signal an operator
	// is querying for — so the degradation must be visible in the logs.
	passCounts := map[string]int64{}
	rows, err := h.DB.QueryContext(ctx, `
SELECT pass_name, COUNT(*) FROM pipeline_state WHERE status='done' GROUP BY pass_name`)
	if err != nil {
		slog.Warn("status: pass-count query failed — pass_counts omitted", "error", err)
	} else {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var name string
			var n int64
			if err := rows.Scan(&name, &n); err == nil {
				passCounts[name] = n
			}
		}
		if err := rows.Err(); err != nil {
			slog.Warn("status: pass-count iteration failed — pass_counts may be incomplete", "error", err)
		}
	}

	// D28b.5 (pre-mortem Story 2): surface per-pass failure counts via
	// synthetic '<pass>_failed' integer keys in the same map — no proto
	// change (decision D4). The end-of-Run top-3 error-prefix histogram
	// in pipeline.Runner carries the causes; these keys carry the rate.
	frows, err := h.DB.QueryContext(ctx, `
SELECT pass_name || '_failed', COUNT(*) FROM pipeline_state WHERE status='failed' GROUP BY pass_name`)
	if err != nil {
		slog.Warn("status: failure-count query failed — '<pass>_failed' counts omitted", "error", err)
	} else {
		defer func() { _ = frows.Close() }()
		for frows.Next() {
			var name string
			var n int64
			if err := frows.Scan(&name, &n); err == nil {
				passCounts[name] = n
			}
		}
		if err := frows.Err(); err != nil {
			slog.Warn("status: failure-count iteration failed — '<pass>_failed' counts may be incomplete", "error", err)
		}
	}

	// Q2 (actor-observability): surface per-pass skip counts via synthetic
	// '<pass>_skipped' keys, mirroring the '<pass>_failed' block above. The
	// runner marks non-indexable files (images, media, binary content/documents)
	// 'skipped'; without these keys the skipped population is invisible and
	// FilesTotal − FilesIndexed silently conflates skipped, failed, and pending.
	srows, err := h.DB.QueryContext(ctx, `
SELECT pass_name || '_skipped', COUNT(*) FROM pipeline_state WHERE status='skipped' GROUP BY pass_name`)
	if err != nil {
		slog.Warn("status: skip-count query failed — '<pass>_skipped' counts omitted", "error", err)
	} else {
		defer func() { _ = srows.Close() }()
		for srows.Next() {
			var name string
			var n int64
			if err := srows.Scan(&name, &n); err == nil {
				passCounts[name] = n
			}
		}
		if err := srows.Err(); err != nil {
			slog.Warn("status: skip-count iteration failed — '<pass>_skipped' counts may be incomplete", "error", err)
		}
	}

	// Watch-mode freshness observability (Phase 6 / D14): surface the pending
	// backlog and its oldest age via synthetic passCounts keys (no proto change,
	// mirroring the '<pass>_failed'/'<pass>_skipped' convention). 'pending_files'
	// is the count of non-deleted files with no embeddings=done row;
	// 'oldest_pending_seconds' is now − the oldest such file's mtime (0 when the
	// backlog is empty). These are meaningful in batch mode too, so they are
	// emitted unconditionally.
	var pendingFiles int64
	if err := h.DB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM files f
WHERE f.deleted_at IS NULL
  AND NOT EXISTS (
    SELECT 1 FROM pipeline_state ps
    WHERE ps.file_id=f.file_id AND ps.pass_name='embeddings' AND ps.status='done')`).Scan(&pendingFiles); err != nil {
		slog.Warn("status: pending_files query failed — count omitted", "error", err)
	} else {
		passCounts["pending_files"] = pendingFiles
		if pendingFiles > 0 {
			var oldestMtime int64
			if err := h.DB.QueryRowContext(ctx, `
SELECT COALESCE(MIN(f.mtime), 0) FROM files f
WHERE f.deleted_at IS NULL
  AND NOT EXISTS (
    SELECT 1 FROM pipeline_state ps
    WHERE ps.file_id=f.file_id AND ps.pass_name='embeddings' AND ps.status='done')`).Scan(&oldestMtime); err != nil {
				slog.Warn("status: oldest_pending query failed — age omitted", "error", err)
			} else if oldestMtime > 0 {
				age := time.Now().Unix() - oldestMtime
				if age < 0 {
					age = 0
				}
				passCounts["oldest_pending_seconds"] = age
			}
		} else {
			passCounts["oldest_pending_seconds"] = 0
		}
	}

	// Watch-loop process counters (D14 round-2 finding #3): only present when the
	// daemon runs in --watch mode. events_reconciled_total counts files first
	// indexed by a reconciliation sweep (the channel-drop safety net firing);
	// watch_overflow_recovery_age_seconds is the staleness of the oldest such
	// recovery, making a dropped-event SLO violation observable rather than silent.
	if h.Watch != nil {
		passCounts["events_reconciled_total"] = h.Watch.EventsReconciledTotal()
		recAt := h.Watch.OverflowRecoveryAgeUnix()
		var recAge int64
		if recAt > 0 {
			recAge = time.Now().Unix() - recAt
			if recAge < 0 {
				recAge = 0
			}
		}
		passCounts["watch_overflow_recovery_age_seconds"] = recAge
	}

	var uuid string
	_ = h.DB.QueryRowContext(ctx, `SELECT value FROM config WHERE key='instance_uuid'`).Scan(&uuid)

	uptime := int64(0)
	if !h.StartTime.IsZero() {
		uptime = int64(time.Since(h.StartTime).Seconds())
	}

	return &pb.StatusResponse{
		FilesTotal:    total,
		FilesIndexed:  indexed,
		PassCounts:    passCounts,
		UptimeSeconds: uptime,
		InstanceUuid:  uuid,
	}, nil
}

func (h *Handler) Health(ctx context.Context, _ *pb.HealthRequest) (*pb.HealthResponse, error) {
	resp := &pb.HealthResponse{}

	var qc string
	err := h.DB.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&qc)
	resp.DbQuickCheckOk = err == nil && qc == "ok"

	var ic string
	err = h.DB.QueryRowContext(ctx, `PRAGMA integrity_check(1)`).Scan(&ic)
	resp.IntegrityCheckOk = err == nil && ic == "ok"

	if !resp.DbQuickCheckOk {
		resp.ErrorMessage = "quick_check failed: " + qc
	} else if !resp.IntegrityCheckOk {
		resp.ErrorMessage = "integrity_check failed: " + ic
	}
	return resp, nil
}
