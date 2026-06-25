//go:build cgo

package query

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// VectorHandler implements VectorSearch using sqlite-vec brute-force KNN.
type VectorHandler struct {
	DB *sql.DB
	// Disabled is set when the stored index was quantized with a scheme
	// incompatible with this server's query encoder (fingerprint mismatch).
	// Vector search then returns a DEGRADED status instead of running KNN on
	// incomparable int8 codes — which would silently return garbage rankings.
	// Set centrally by NewServer so EVERY entry point is gated (the public
	// VectorSearch RPC and Batch accept client-supplied int8 codes and never
	// touch the embedder, so an embedder-nil gate alone does not cover them).
	Disabled bool
}

// Search uses sqlite-vec's KNN: the vec0 virtual table is queried with
// MATCH on the embedding column and k constraint. sqlite-vec returns
// results ordered by distance (lower = closer).
func (h *VectorHandler) Search(ctx context.Context, req *pb.VectorSearchRequest) (*pb.VectorSearchResponse, error) {
	start := time.Now()
	resp := &pb.VectorSearchResponse{Status: &pb.SourceStatus{SourceName: "vector"}}

	if h.Disabled {
		// The stored codes were quantized with a different scheme than this
		// server's encoder; KNN on them would silently return garbage. Refuse
		// here so the raw VectorSearch RPC and Batch are gated too, not just
		// the embedder-driven SearchBroadly path.
		resp.Status.Status = "DEGRADED"
		resp.Status.ErrorMessage = "semantic search disabled: index quantization is incompatible with this server — rebuild the index"
		resp.Status.LatencyMs = int32(time.Since(start).Milliseconds())
		return resp, nil
	}

	if len(req.QueryEmbeddingInt8) != 384 {
		resp.Status.Status = "CONTRACT_ERROR"
		resp.Status.ErrorMessage = fmt.Sprintf("embedding len %d, want 384", len(req.QueryEmbeddingInt8))
		resp.Status.LatencyMs = int32(time.Since(start).Milliseconds())
		return resp, nil
	}
	k := clampK(int(req.K))

	// sqlite-vec KNN: MATCH provides the query vector, k constraint limits results.
	// The distance column is returned automatically by vec0.
	//
	// FIX 1b: join through nodes to files and filter n.deleted_at/f.deleted_at in the
	// candidate selection BEFORE the LIMIT k. Soft-delete sets ONLY files.deleted_at
	// (the chunk/node deleted_at stay NULL until the hard purge), so without this
	// filter a soft-deleted file's chunks rank into the top-K, consume result slots,
	// and crowd out live hits — hydrateNodes then drops their content but the wasted
	// slot makes the result window short. Filtering here keeps soft-deleted files out
	// of the top-K entirely. sqlite-vec requires the k constraint inside the vec0
	// MATCH, so the deleted-at predicates are applied as an outer filter over the KNN
	// result; k is over-fetched to keep a full live window. clampK caps k at 100, so
	// 2*k is bounded.
	rows, err := h.DB.QueryContext(ctx, `
SELECT chunk_id, node_id, distance FROM (
  SELECT e.chunk_id AS chunk_id, c.node_id AS node_id, e.distance AS distance
  FROM embeddings e
  JOIN chunks c ON c.chunk_id = e.chunk_id
  JOIN nodes n ON n.node_id = c.node_id
  JOIN files f ON f.file_id = n.file_id
  WHERE e.embedding MATCH vec_int8(?)
    AND e.k = ?
    AND c.deleted_at IS NULL AND n.deleted_at IS NULL AND f.deleted_at IS NULL
)
ORDER BY distance ASC
LIMIT ?`, req.QueryEmbeddingInt8, 2*k, k)
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
			return nil, status.Errorf(codes.Internal, "vector scan: %v", err)
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
			return nil, status.Errorf(codes.Internal, "hydrate: %v", err)
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
