//go:build cgo

// Package admin contains IndexAdmin RPC handlers (Status + Health in M1).
package admin

import (
	"context"
	"database/sql"
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

	passCounts := map[string]int64{}
	rows, err := h.DB.QueryContext(ctx, `
SELECT pass_name, COUNT(*) FROM pipeline_state WHERE status='done' GROUP BY pass_name`)
	if err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var name string
			var n int64
			if err := rows.Scan(&name, &n); err == nil {
				passCounts[name] = n
			}
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
