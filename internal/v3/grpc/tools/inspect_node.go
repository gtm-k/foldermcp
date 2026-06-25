//go:build cgo

package tools

import (
	"context"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
)

// Inspect retrieves a single node by ID with detail-driven chunk hydration.
// detail levels: brief=1 chunk, standard=3 chunks, full=20 chunks.
func (h *InspectHandler) Inspect(ctx context.Context, req *pb.InspectNodeRequest) (*pb.InspectNodeResponse, error) {
	detail := req.Detail
	if detail == "" {
		detail = "brief"
	}
	chunksPer := 1
	switch detail {
	case "standard":
		chunksPer = 3
	case "full":
		chunksPer = 20
	}
	gnReq := &pb.GetNodesRequest{
		NodeIds: []int64{req.NodeId},
		Hydrate: &pb.HydrationHint{Chunks: true, Metadata: true, Provenance: true, ChunksPerNode: int32(chunksPer)},
	}
	resp, err := h.Nodes.Get(ctx, gnReq)
	if err != nil {
		return nil, err
	}
	if len(resp.Nodes) == 0 {
		return &pb.InspectNodeResponse{
			Status: &pb.SourceStatus{SourceName: "inspect", Status: "EMPTY_BUT_EXECUTED"},
		}, nil
	}
	// Layer 3 EGRESS redaction (D17): same choke point as SearchBroadly — full
	// Sanitizer over the hydrated node's chunk text before it leaves the daemon
	// (defense-in-depth over pre-Phase-7 indexes).
	node := resp.Nodes[0]
	redactHydratedNode(node)
	return &pb.InspectNodeResponse{
		Node:   node,
		Status: &pb.SourceStatus{SourceName: "inspect", Status: "OK"},
	}, nil
}
