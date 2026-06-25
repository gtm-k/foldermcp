//go:build cgo

package query

import (
	"context"
	"database/sql"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
)

// GraphHandler is a stub in M1 — the edges table is empty because
// cross-file linking and Louvain community detection are M2 work.
type GraphHandler struct {
	DB *sql.DB
}

func (h *GraphHandler) Expand(ctx context.Context, req *pb.GraphExpandRequest) (*pb.GraphExpandResponse, error) {
	return &pb.GraphExpandResponse{
		Status: &pb.SourceStatus{
			SourceName: "graph",
			Status:     "EMPTY_BUT_EXECUTED",
			LatencyMs:  0,
		},
	}, nil
}
