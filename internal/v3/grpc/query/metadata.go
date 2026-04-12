//go:build cgo

package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
)

// MetadataHandler searches nodes filtered by content_class, path_prefix, and mtime range.
type MetadataHandler struct {
	DB *sql.DB
}

func (h *MetadataHandler) Search(ctx context.Context, req *pb.MetadataSearchRequest) (*pb.MetadataSearchResponse, error) {
	start := time.Now()
	resp := &pb.MetadataSearchResponse{Status: &pb.SourceStatus{SourceName: "metadata"}}
	k := clampK(int(req.K))

	var conditions []string
	var args []any

	conditions = append(conditions, "f.deleted_at IS NULL", "n.deleted_at IS NULL")

	if req.ContentClass != "" {
		conditions = append(conditions, "f.content_class = ?")
		args = append(args, req.ContentClass)
	}
	if req.PathPrefix != "" {
		conditions = append(conditions, "f.path LIKE ?")
		args = append(args, req.PathPrefix+"%")
	}
	if req.MtimeFrom > 0 {
		conditions = append(conditions, "f.mtime >= ?")
		args = append(args, req.MtimeFrom)
	}
	if req.MtimeTo > 0 {
		conditions = append(conditions, "f.mtime <= ?")
		args = append(args, req.MtimeTo)
	}
	args = append(args, k)

	q := fmt.Sprintf(`
SELECT n.node_id, f.mtime
FROM files f JOIN nodes n ON n.file_id = f.file_id
WHERE %s
ORDER BY f.mtime DESC
LIMIT ?`, strings.Join(conditions, " AND "))

	rows, err := h.DB.QueryContext(ctx, q, args...)
	if err != nil {
		resp.Status.Status = "DEGRADED"
		resp.Status.ErrorMessage = err.Error()
		resp.Status.LatencyMs = int32(time.Since(start).Milliseconds())
		return resp, nil
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var nodeID, mtime int64
		if err := rows.Scan(&nodeID, &mtime); err != nil {
			return nil, err
		}
		resp.Results = append(resp.Results, &pb.ScoredNode{
			NodeId: nodeID,
			Score:  float32(mtime), // recency as score
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if req.Hydrate != nil {
		if err := hydrateNodes(ctx, h.DB, resp.Results, req.Hydrate); err != nil {
			return nil, err
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
