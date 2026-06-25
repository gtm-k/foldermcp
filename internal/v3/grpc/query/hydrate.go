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

// hydrateNodes fills HydratedNode on each ScoredNode per the hint. It is the
// shared egress choke point for the vector and FTS search paths (both call it),
// so its WHERE clause is where soft-deleted exclusion is enforced once for both.
//
// FIX 2: the WHERE clause filters BOTH n.deleted_at IS NULL AND f.deleted_at IS
// NULL. A6 watch reconcile soft-deletes a dropped file (sets files.deleted_at) and
// hard-purges only at the NEXT reconcile; in between, the file's NODE rows still
// have deleted_at NULL. Filtering only n.deleted_at would leave such a node
// hydratable (path/name/chunks leaked) for up to one reconcile interval. Adding
// f.deleted_at IS NULL excludes it IMMEDIATELY the instant reconcile flags the
// file. A node whose file is soft-deleted simply does not land in nodeByID, so its
// ScoredNode keeps a nil Hydrated and fetchTopChunks is never called for it (no
// chunk content can escape via the chunk path either).
func hydrateNodes(ctx context.Context, db *sql.DB, scored []*pb.ScoredNode, hint *pb.HydrationHint) error {
	if len(scored) == 0 {
		return nil
	}
	ids := make([]any, len(scored))
	placeholders := make([]byte, 0, 2*len(scored))
	for i, s := range scored {
		ids[i] = s.NodeId
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
	}
	q := fmt.Sprintf(`
SELECT n.node_id, n.file_id, f.path, n.node_type, n.name, f.content_class,
       COALESCE(n.language,''), n.provenance, COALESCE(n.confidence, 0.0),
       n.properties
FROM nodes n JOIN files f ON f.file_id = n.file_id
WHERE n.node_id IN (%s) AND n.deleted_at IS NULL AND f.deleted_at IS NULL`, placeholders)
	rows, err := db.QueryContext(ctx, q, ids...)
	if err != nil {
		return status.Errorf(codes.Internal, "hydrate query: %v", err)
	}
	defer func() { _ = rows.Close() }()

	nodeByID := make(map[int64]*pb.HydratedNode)
	for rows.Next() {
		h := &pb.HydratedNode{}
		if err := rows.Scan(&h.NodeId, &h.FileId, &h.Path, &h.NodeType, &h.Name,
			&h.ContentClass, &h.Language, &h.Provenance, &h.Confidence, &h.PropertiesJson); err != nil {
			return status.Errorf(codes.Internal, "hydrate scan: %v", err)
		}
		// MED-3 / F1: PropertiesJson can carry a secret in a Go symbol signature
		// (e.g. `const apiKey = "..."`). Redact STRUCTURALLY at this shared choke
		// point so SearchBroadly/Inspect inherit it AND the egressed value stays
		// valid JSON; node chunks are flat text, redacted in fetchTopChunks below.
		h.PropertiesJson = redactPropertiesJSON(h.PropertiesJson, "node_id", h.NodeId)
		nodeByID[h.NodeId] = h
	}
	if err := rows.Err(); err != nil {
		return err
	}

	chunksPerNode := int(hint.ChunksPerNode)
	if chunksPerNode == 0 {
		chunksPerNode = 2
	}
	if hint.Chunks {
		for id, h := range nodeByID {
			chunks, err := fetchTopChunks(ctx, db, id, chunksPerNode)
			if err != nil {
				return err
			}
			h.Chunks = chunks
		}
	}
	for _, s := range scored {
		if h, ok := nodeByID[s.NodeId]; ok {
			s.Hydrated = h
		}
	}
	return nil
}

func fetchTopChunks(ctx context.Context, db *sql.DB, nodeID int64, limit int) ([]*pb.HydratedChunk, error) {
	rows, err := db.QueryContext(ctx, `
SELECT chunk_id, text, token_count, chunk_kind
FROM chunks WHERE node_id=? AND deleted_at IS NULL
ORDER BY chunk_id LIMIT ?`, nodeID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*pb.HydratedChunk
	for rows.Next() {
		c := &pb.HydratedChunk{}
		if err := rows.Scan(&c.ChunkId, &c.Text, &c.TokenCount, &c.ChunkKind); err != nil {
			return nil, err
		}
		// MED-3: shared egress choke point — every chunk hydrated here (tools-layer
		// SearchBroadly/Inspect AND IndexQuery GetNodes-with-hydrate) inherits
		// egress redaction.
		redactHydratedChunk(c)
		out = append(out, c)
	}
	return out, rows.Err()
}
