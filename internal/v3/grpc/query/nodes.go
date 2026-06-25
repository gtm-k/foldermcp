//go:build cgo

package query

import (
	"context"
	"database/sql"
	"fmt"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// NodesHandler implements GetNodes — fetch specific nodes by ID.
type NodesHandler struct {
	DB *sql.DB
}

func (h *NodesHandler) Get(ctx context.Context, req *pb.GetNodesRequest) (*pb.GetNodesResponse, error) {
	if len(req.NodeIds) == 0 {
		return &pb.GetNodesResponse{}, nil
	}

	placeholders := make([]byte, 0, 2*len(req.NodeIds))
	args := make([]any, len(req.NodeIds))
	for i, id := range req.NodeIds {
		args[i] = id
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
	}

	// FIX 2: filter f.deleted_at IS NULL too (not only n.deleted_at). GetNodes is a
	// by-id fetch that returns hydrated content without going through hydrateNodes,
	// so it needs the same soft-deleted-file exclusion the hydrate choke point now
	// enforces — otherwise a node whose file was soft-deleted by watch reconcile
	// (node.deleted_at still NULL until the next purge) would leak via this path.
	q := fmt.Sprintf(`
SELECT n.node_id, n.file_id, f.path, n.node_type, n.name, f.content_class,
       COALESCE(n.language,''), n.provenance, COALESCE(n.confidence, 0.0),
       n.properties
FROM nodes n JOIN files f ON f.file_id = n.file_id
WHERE n.node_id IN (%s) AND n.deleted_at IS NULL AND f.deleted_at IS NULL`, placeholders)

	rows, err := h.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get_nodes query: %v", err)
	}
	defer func() { _ = rows.Close() }()

	resp := &pb.GetNodesResponse{}
	for rows.Next() {
		n := &pb.HydratedNode{}
		if err := rows.Scan(&n.NodeId, &n.FileId, &n.Path, &n.NodeType, &n.Name,
			&n.ContentClass, &n.Language, &n.Provenance, &n.Confidence, &n.PropertiesJson); err != nil {
			return nil, status.Errorf(codes.Internal, "get_nodes scan: %v", err)
		}
		// MED-3 / F1: GetNodes is a low-level IndexQuery RPC that previously
		// returned a RAW PropertiesJson — which can carry a secret in a Go symbol
		// signature. Redact STRUCTURALLY so the egressed value stays valid JSON
		// (flat-text redaction would consume the closing quote/brace). Hydrated
		// chunks (below) are flat text, redacted in fetchTopChunks.
		n.PropertiesJson = redactPropertiesJSON(n.PropertiesJson, "node_id", n.NodeId)
		resp.Nodes = append(resp.Nodes, n)
	}

	// Optionally hydrate chunks
	if req.Hydrate != nil && req.Hydrate.Chunks {
		chunksPerNode := int(req.Hydrate.ChunksPerNode)
		if chunksPerNode == 0 {
			chunksPerNode = 2
		}
		for _, n := range resp.Nodes {
			chunks, err := fetchTopChunks(ctx, h.DB, n.NodeId, chunksPerNode)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "get_nodes hydrate: %v", err)
			}
			n.Chunks = chunks
		}
	}

	if err := rows.Err(); err != nil {
		return nil, status.Errorf(codes.Internal, "get_nodes rows: %v", err)
	}
	return resp, nil
}
