//go:build cgo

package query

import (
	"context"
	"fmt"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// BatchHandler dispatches individual ops to their handlers, capturing
// per-op errors without aborting the whole batch.
type BatchHandler struct {
	Vector   *VectorHandler
	FTS      *FTSHandler
	Filename *FilenameHandler
	Metadata *MetadataHandler
	Graph    *GraphHandler
	Nodes    *NodesHandler
	Chunks   *ChunksHandler
}

const (
	MaxBatchOps = 32
)

func (h *BatchHandler) Batch(ctx context.Context, req *pb.BatchRequest) (*pb.BatchResponse, error) {
	if len(req.Ops) > MaxBatchOps {
		return nil, status.Errorf(codes.InvalidArgument, "batch: %d ops exceeds max %d", len(req.Ops), MaxBatchOps)
	}
	out := &pb.BatchResponse{}
	for _, op := range req.Ops {
		res := &pb.BatchResult{}
		switch v := op.Op.(type) {
		case *pb.BatchOp_Vector:
			r, err := h.Vector.Search(ctx, v.Vector)
			if err != nil {
				res.Error = err.Error()
			} else {
				res.Result = &pb.BatchResult_Vector{Vector: r}
			}
		case *pb.BatchOp_Fts:
			r, err := h.FTS.Search(ctx, v.Fts)
			if err != nil {
				res.Error = err.Error()
			} else {
				res.Result = &pb.BatchResult_Fts{Fts: r}
			}
		case *pb.BatchOp_Filename:
			r, err := h.Filename.Search(ctx, v.Filename)
			if err != nil {
				res.Error = err.Error()
			} else {
				res.Result = &pb.BatchResult_Filename{Filename: r}
			}
		case *pb.BatchOp_Metadata:
			r, err := h.Metadata.Search(ctx, v.Metadata)
			if err != nil {
				res.Error = err.Error()
			} else {
				res.Result = &pb.BatchResult_Metadata{Metadata: r}
			}
		case *pb.BatchOp_GraphExpand:
			r, err := h.Graph.Expand(ctx, v.GraphExpand)
			if err != nil {
				res.Error = err.Error()
			} else {
				res.Result = &pb.BatchResult_GraphExpand{GraphExpand: r}
			}
		case *pb.BatchOp_GetNodes:
			r, err := h.Nodes.Get(ctx, v.GetNodes)
			if err != nil {
				res.Error = err.Error()
			} else {
				res.Result = &pb.BatchResult_GetNodes{GetNodes: r}
			}
		case *pb.BatchOp_GetChunks:
			r, err := h.Chunks.Get(ctx, v.GetChunks)
			if err != nil {
				res.Error = err.Error()
			} else {
				res.Result = &pb.BatchResult_GetChunks{GetChunks: r}
			}
		default:
			res.Error = "unknown batch op type"
		}
		if res.Error != "" {
			out.Warnings = append(out.Warnings, fmt.Sprintf("op %d failed: %s", len(out.Results), res.Error))
		}
		out.Results = append(out.Results, res)
	}
	return out, nil
}
