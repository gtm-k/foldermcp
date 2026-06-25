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
// hydrateNodes fills HydratedNode on each ScoredNode per the hint.
//
// FINDING 1 (retrieval quality): matchedChunks maps node_id -> the best-scoring chunk_id
// that the chunk-level source (vector/FTS) actually matched for that node. A4/A5 made a
// whole PDF/DOCX/CSV ONE node holding MANY chunks, so a query matching chunk #50 of a
// 200-chunk node must SHOW chunk #50 — not Chunks[0] (the title page / CSV schema header,
// which is what LIMIT chunksPerNode ORDER BY chunk_id returns). When a node has a matched
// chunk_id (>0), fetchTopChunks fetches THAT chunk first plus neighbors, so it is both
// included and Chunks[0] (the snippet source). matchedChunks may be nil (filename/metadata/
// GetNodes paths have no chunk-level match) — those keep the M1 Chunks[0] behaviour.
func hydrateNodes(ctx context.Context, db *sql.DB, scored []*pb.ScoredNode, hint *pb.HydrationHint, matchedChunks map[int64]int64) error {
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
			chunks, err := fetchTopChunks(ctx, db, id, chunksPerNode, matchedChunks[id])
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

// fetchTopChunks returns up to `limit` chunks for a node.
//
// FINDING 1 (retrieval quality): when matchedChunkID > 0, the matched chunk is INCLUDED
// and placed FIRST, then up to limit-1 neighbours fill the rest (ORDER BY chunk_id). This
// guarantees a query matching chunk #50 of a 200-chunk document node both hydrates chunk
// #50 and surfaces it as Chunks[0] (the snippet source), instead of being truncated away
// by `LIMIT limit ORDER BY chunk_id` — which would only ever return the title page /
// schema header. When matchedChunkID == 0 (filename/metadata/GetNodes — no chunk-level
// match), the original ORDER BY chunk_id LIMIT behaviour is preserved exactly.
//
// FIX 1c invariant preserved: the chunks query joins nodes + files and filters
// c/n/f.deleted_at on EVERY branch, so the soft-deleted-file egress filter holds at this
// path regardless of caller.
func fetchTopChunks(ctx context.Context, db *sql.DB, nodeID int64, limit int, matchedChunkID int64) ([]*pb.HydratedChunk, error) {
	if matchedChunkID > 0 {
		return fetchTopChunksWithMatch(ctx, db, nodeID, limit, matchedChunkID)
	}
	// FIX 1c: join nodes + files and filter n.deleted_at/f.deleted_at here too, for
	// defense-in-depth uniformity. Current callers (hydrateNodes, GetNodes) already
	// pre-filter the node by file, so this is redundant for them — but it makes the
	// chunk-egress invariant hold AT THIS PATH regardless of caller, so a future
	// caller that fetches chunks for a soft-deleted file's node cannot reintroduce
	// the leak. soft-delete sets ONLY files.deleted_at, hence the f.deleted_at term.
	rows, err := db.QueryContext(ctx, `
SELECT c.chunk_id, c.text, c.token_count, c.chunk_kind
FROM chunks c
JOIN nodes n ON n.node_id = c.node_id
JOIN files f ON f.file_id = n.file_id
WHERE c.node_id=?
  AND c.deleted_at IS NULL AND n.deleted_at IS NULL AND f.deleted_at IS NULL
ORDER BY c.chunk_id LIMIT ?`, nodeID, limit)
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

// fetchTopChunksWithMatch fetches the matched chunk FIRST, then up to limit-1 neighbour
// chunks for context (ORDER BY chunk_id), de-duplicating the matched chunk if it also
// falls in the neighbour window. It carries the SAME c/n/f.deleted_at egress filter as
// fetchTopChunks (a soft-deleted file's chunk — incl. the "matched" one — is never
// returned), and applies the same redactHydratedChunk egress redaction.
func fetchTopChunksWithMatch(ctx context.Context, db *sql.DB, nodeID int64, limit int, matchedChunkID int64) ([]*pb.HydratedChunk, error) {
	if limit < 1 {
		limit = 1
	}
	// The matched chunk is returned first (rank 0); the remaining limit-1 slots are the
	// node's lowest-chunk_id chunks for stable context, matching the prior ORDER BY
	// chunk_id window. UNION + the outer ORDER BY rank keeps the matched chunk at the
	// head; the per-row deleted_at filter is identical on both legs.
	const q = `
SELECT chunk_id, text, token_count, chunk_kind, rank FROM (
  SELECT c.chunk_id AS chunk_id, c.text AS text, c.token_count AS token_count,
         c.chunk_kind AS chunk_kind, 0 AS rank
  FROM chunks c
  JOIN nodes n ON n.node_id = c.node_id
  JOIN files f ON f.file_id = n.file_id
  WHERE c.node_id=? AND c.chunk_id=?
    AND c.deleted_at IS NULL AND n.deleted_at IS NULL AND f.deleted_at IS NULL
  UNION
  SELECT c.chunk_id AS chunk_id, c.text AS text, c.token_count AS token_count,
         c.chunk_kind AS chunk_kind, 1 AS rank
  FROM chunks c
  JOIN nodes n ON n.node_id = c.node_id
  JOIN files f ON f.file_id = n.file_id
  WHERE c.node_id=? AND c.chunk_id<>?
    AND c.deleted_at IS NULL AND n.deleted_at IS NULL AND f.deleted_at IS NULL
  ORDER BY rank, chunk_id
  LIMIT ?
)
ORDER BY rank, chunk_id`
	rows, err := db.QueryContext(ctx, q, nodeID, matchedChunkID, nodeID, matchedChunkID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*pb.HydratedChunk
	for rows.Next() {
		c := &pb.HydratedChunk{}
		var rank int
		if err := rows.Scan(&c.ChunkId, &c.Text, &c.TokenCount, &c.ChunkKind, &rank); err != nil {
			return nil, err
		}
		redactHydratedChunk(c)
		out = append(out, c)
	}
	return out, rows.Err()
}
