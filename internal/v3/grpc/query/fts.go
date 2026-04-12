//go:build cgo

package query

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
)

// FTSHandler implements FTSSearch against the chunks_fts FTS5 virtual table.
type FTSHandler struct {
	DB *sql.DB
}

func (h *FTSHandler) Search(ctx context.Context, req *pb.FTSSearchRequest) (*pb.FTSSearchResponse, error) {
	start := time.Now()
	resp := &pb.FTSSearchResponse{
		Status: &pb.SourceStatus{SourceName: "fts"},
	}
	k := clampK(int(req.K))

	// FTS5 BM25: bm25() returns negative values where lower (more negative)
	// means better match. Negating produces a positive score where higher = better.
	rows, err := h.DB.QueryContext(ctx, `
SELECT c.chunk_id, c.node_id, -bm25(chunks_fts) AS score
FROM chunks_fts
JOIN chunks c ON c.chunk_id = chunks_fts.rowid
WHERE chunks_fts MATCH ?
ORDER BY score DESC
LIMIT ?`, req.Query, k)
	if err != nil {
		resp.Status.Status = "DEGRADED"
		resp.Status.ErrorMessage = err.Error()
		resp.Status.LatencyMs = int32(time.Since(start).Milliseconds())
		return resp, nil
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var chunkID, nodeID int64
		var score float64
		if err := rows.Scan(&chunkID, &nodeID, &score); err != nil {
			resp.Status.Status = "CONTRACT_ERROR"
			resp.Status.ErrorMessage = err.Error()
			resp.Status.LatencyMs = int32(time.Since(start).Milliseconds())
			return resp, nil
		}
		resp.Results = append(resp.Results, &pb.ScoredNode{
			NodeId: nodeID,
			Score:  float32(score),
		})
	}
	if err := rows.Err(); err != nil {
		resp.Status.Status = "CONTRACT_ERROR"
		resp.Status.ErrorMessage = err.Error()
		resp.Status.LatencyMs = int32(time.Since(start).Milliseconds())
		return resp, nil
	}

	if req.Hydrate != nil {
		if err := hydrateNodes(ctx, h.DB, resp.Results, req.Hydrate); err != nil {
			return nil, fmt.Errorf("hydrate: %w", err)
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

// clampK normalises the user-supplied k to [1, 100] with default 20.
func clampK(k int) int {
	if k <= 0 {
		return 20
	}
	if k > 100 {
		return 100
	}
	return k
}
