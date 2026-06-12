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

// Handler implements IndexAdmin.Status and IndexAdmin.Health.
type Handler struct {
	DB        *sql.DB
	StartTime time.Time
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
