//go:build cgo

package query

import (
	"context"
	"database/sql"
	"strings"
	"time"
	"unicode"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

	// The raw user query must never reach FTS5 MATCH directly: natural-language
	// punctuation (?, :, -, quotes) is parsed as FTS5 query syntax and raises a
	// hard "syntax error", and the default implicit-AND between bare terms makes
	// any multi-word sentence require every term in one chunk. Both collapse
	// real queries to zero results (the M1/G5 "FTS returns 0" defect). Build a
	// sanitized OR-of-quoted-terms expression instead.
	match, ok := buildFTSMatch(req.Query)
	if !ok {
		// No usable terms — execute nothing, report honest emptiness.
		resp.Status.Status = "EMPTY_BUT_EXECUTED"
		resp.Status.LatencyMs = int32(time.Since(start).Milliseconds())
		return resp, nil
	}

	// FTS5 BM25: bm25() returns negative values where lower (more negative)
	// means better match. Negating produces a positive score where higher = better.
	//
	// FIX 1b: join through nodes to files and filter n.deleted_at/f.deleted_at in the
	// candidate selection BEFORE the LIMIT k. Soft-delete sets ONLY files.deleted_at
	// (chunk/node deleted_at stay NULL until the hard purge), so without this filter a
	// soft-deleted file's chunks rank into the top-K, consume result slots, and crowd
	// out live hits — hydrateNodes then drops their content but the wasted slot makes
	// the window short. Filtering here keeps soft-deleted files out of the top-K.
	rows, err := h.DB.QueryContext(ctx, `
SELECT c.chunk_id, c.node_id, -bm25(chunks_fts) AS score
FROM chunks_fts
JOIN chunks c ON c.chunk_id = chunks_fts.rowid
JOIN nodes n ON n.node_id = c.node_id
JOIN files f ON f.file_id = n.file_id
WHERE chunks_fts MATCH ?
  AND c.deleted_at IS NULL AND n.deleted_at IS NULL AND f.deleted_at IS NULL
ORDER BY score DESC
LIMIT ?`, match, k)
	if err != nil {
		resp.Status.Status = "DEGRADED"
		resp.Status.ErrorMessage = err.Error()
		resp.Status.LatencyMs = int32(time.Since(start).Milliseconds())
		return resp, nil
	}
	defer func() { _ = rows.Close() }()

	// FINDING 1 (retrieval quality): FTS matches at the CHUNK level (chunks_fts.rowid =
	// chunk_id) but ScoredNode only carries node_id. Capture, per node, the best-scoring
	// matched chunk_id (rows are ordered by BM25 score DESC, so the FIRST row for a node
	// is its best-matching chunk) and thread it INTERNALLY into hydration so the matched
	// chunk is hydrated and surfaced as the snippet instead of the lowest-chunk_id
	// Chunks[0]. No proto field is added; the map never leaves this package.
	matchedChunks := make(map[int64]int64)
	for rows.Next() {
		var chunkID, nodeID int64
		var score float64
		if err := rows.Scan(&chunkID, &nodeID, &score); err != nil {
			resp.Status.Status = "CONTRACT_ERROR"
			resp.Status.ErrorMessage = err.Error()
			resp.Status.LatencyMs = int32(time.Since(start).Milliseconds())
			return resp, nil
		}
		if _, seen := matchedChunks[nodeID]; !seen {
			// First (highest-BM25) row for this node = its best matched chunk.
			matchedChunks[nodeID] = chunkID
			resp.Results = append(resp.Results, &pb.ScoredNode{
				NodeId: nodeID,
				Score:  float32(score),
			})
		}
	}
	if err := rows.Err(); err != nil {
		resp.Status.Status = "CONTRACT_ERROR"
		resp.Status.ErrorMessage = err.Error()
		resp.Status.LatencyMs = int32(time.Since(start).Milliseconds())
		return resp, nil
	}

	if req.Hydrate != nil {
		if err := hydrateNodes(ctx, h.DB, resp.Results, req.Hydrate, matchedChunks); err != nil {
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

const (
	// maxQueryBytes bounds the raw query we tokenize and maxFTSTerms bounds the
	// number of OR clauses in the MATCH expression. Together they cap FTS5
	// parse/allocation cost so a single oversized query cannot exhaust daemon
	// resources (Codex review: UNBOUNDED_MATCH_EXPRESSION). A real query never
	// approaches these limits; they only clip pathological input.
	maxQueryBytes = 4096
	maxFTSTerms   = 64
)

// buildFTSMatch converts a raw user query into a safe FTS5 MATCH expression.
// Each alphanumeric token is wrapped as an FTS5 string literal (double quotes,
// internal quotes doubled), which neutralises every FTS5 operator/column/
// special character — so user punctuation can never become query syntax. The
// tokens are combined with OR so any single term match contributes a hit; BM25
// ranks the results and the broader hybrid pipeline (RRF over FTS + vector)
// supplies precision. Returns ok=false when the query yields no usable terms,
// letting the caller short-circuit to EMPTY_BUT_EXECUTED instead of issuing a
// malformed MATCH.
func buildFTSMatch(raw string) (string, bool) {
	if len(raw) > maxQueryBytes {
		raw = raw[:maxQueryBytes]
	}
	terms := ftsTokenize(raw)
	if len(terms) == 0 {
		return "", false
	}
	if len(terms) > maxFTSTerms {
		terms = terms[:maxFTSTerms]
	}
	quoted := make([]string, len(terms))
	for i, t := range terms {
		quoted[i] = `"` + strings.ReplaceAll(t, `"`, `""`) + `"`
	}
	return strings.Join(quoted, " OR "), true
}

// ftsTokenize splits a query into terms on any non-alphanumeric rune, matching
// the token boundaries the unicode61 tokenizer uses for indexed content.
func ftsTokenize(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
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
