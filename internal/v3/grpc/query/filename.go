//go:build cgo

package query

import (
	"context"
	"database/sql"
	"strings"
	"time"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// FilenameHandler searches files.path via case-insensitive substring matching.
type FilenameHandler struct {
	DB *sql.DB
}

func (h *FilenameHandler) Search(ctx context.Context, req *pb.FilenameSearchRequest) (*pb.FilenameSearchResponse, error) {
	start := time.Now()
	resp := &pb.FilenameSearchResponse{Status: &pb.SourceStatus{SourceName: "filename"}}
	k := clampK(int(req.K))

	tokens := strings.Fields(strings.ToLower(req.Query))
	if len(tokens) == 0 {
		resp.Status.Status = "EMPTY_BUT_EXECUTED"
		resp.Status.LatencyMs = int32(time.Since(start).Milliseconds())
		return resp, nil
	}

	var conditions []string
	var args []any
	for _, tok := range tokens {
		conditions = append(conditions, "LOWER(path) LIKE ?")
		args = append(args, "%"+tok+"%")
	}
	args = append(args, k)

	q := `
SELECT f.file_id, n.node_id, LENGTH(f.path) AS plen
FROM files f JOIN nodes n ON n.file_id = f.file_id
WHERE ` + strings.Join(conditions, " AND ") + `
  AND f.deleted_at IS NULL AND n.deleted_at IS NULL
ORDER BY plen ASC
LIMIT ?`

	rows, err := h.DB.QueryContext(ctx, q, args...)
	if err != nil {
		resp.Status.Status = "DEGRADED"
		resp.Status.ErrorMessage = err.Error()
		resp.Status.LatencyMs = int32(time.Since(start).Milliseconds())
		return resp, nil
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var fileID, nodeID int64
		var plen int
		if err := rows.Scan(&fileID, &nodeID, &plen); err != nil {
			return nil, status.Errorf(codes.Internal, "filename: %v", err)
		}
		score := float32(1.0 / (1.0 + float32(plen)/64.0))
		resp.Results = append(resp.Results, &pb.ScoredNode{NodeId: nodeID, Score: score})
	}
	if err := rows.Err(); err != nil {
		return nil, status.Errorf(codes.Internal, "filename rows: %v", err)
	}

	if req.Hydrate != nil {
		// Filename matches on the path, not a chunk — no matched chunk_id to thread,
		// so hydration keeps the M1 Chunks[0] behaviour (nil matchedChunks map).
		if err := hydrateNodes(ctx, h.DB, resp.Results, req.Hydrate, nil); err != nil {
			return nil, status.Errorf(codes.Internal, "filename: %v", err)
		}
	}

	if len(resp.Results) == 0 {
		resp.Status.Status = "EMPTY_BUT_EXECUTED"
	} else {
		resp.Status.Status = "OK"
	}
	resp.Status.LatencyMs = int32(time.Since(start).Milliseconds())
	return resp, nil
}
