//go:build cgo

package query

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
)

// VectorHandler implements VectorSearch using sqlite-vec brute-force KNN.
type VectorHandler struct {
	DB *sql.DB
}

// Search uses sqlite-vec's KNN: the vec0 virtual table is queried with
// MATCH on the embedding column and k constraint. sqlite-vec returns
// results ordered by distance (lower = closer).
func (h *VectorHandler) Search(ctx context.Context, req *pb.VectorSearchRequest) (*pb.VectorSearchResponse, error) {
	start := time.Now()
	resp := &pb.VectorSearchResponse{Status: &pb.SourceStatus{SourceName: "vector"}}

	if len(req.QueryEmbeddingInt8) != 384 {
		resp.Status.Status = "CONTRACT_ERROR"
		resp.Status.ErrorMessage = fmt.Sprintf("embedding len %d, want 384", len(req.QueryEmbeddingInt8))
		resp.Status.LatencyMs = int32(time.Since(start).Milliseconds())
		return resp, nil
	}
	k := clampK(int(req.K))

	// sqlite-vec KNN: MATCH provides the query vector, k constraint limits results.
	// The distance column is returned automatically by vec0.
	rows, err := h.DB.QueryContext(ctx, `
SELECT e.chunk_id, c.node_id, e.distance
FROM embeddings e
JOIN chunks c ON c.chunk_id = e.chunk_id
WHERE e.embedding MATCH vec_int8(?)
  AND e.k = ?`, req.QueryEmbeddingInt8, k)
	if err != nil {
		resp.Status.Status = "DEGRADED"
		resp.Status.ErrorMessage = err.Error()
		resp.Status.LatencyMs = int32(time.Since(start).Milliseconds())
		return resp, nil
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var chunkID, nodeID int64
		var distance float64
		if err := rows.Scan(&chunkID, &nodeID, &distance); err != nil {
			return nil, err
		}
		// Convert distance (lower=closer) to score (higher=better).
		score := float32(1.0 / (1.0 + distance))
		resp.Results = append(resp.Results, &pb.ScoredNode{
			NodeId: nodeID,
			Score:  score,
		})
	}
	if err := rows.Err(); err != nil {
		resp.Status.Status = "DEGRADED"
		resp.Status.ErrorMessage = err.Error()
		resp.Status.LatencyMs = int32(time.Since(start).Milliseconds())
		return resp, nil
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
