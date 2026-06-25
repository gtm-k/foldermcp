//go:build cgo

package tools

import (
	"context"
	"sort"
	"strings"
	"time"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
)

const (
	deadlineInteractive = 2 * time.Second
	rrfK                = 60 // RRF constant; provisional per spec §10.3
)

// SearchBroadly runs FTS + Filename in parallel first; if Embedder is
// available and mode requests it, also runs VectorSearch. RRF fuses the
// lists, then the result set is shaped by detail level.
func (h *SearchBroadlyHandler) SearchBroadly(ctx context.Context, req *pb.SearchBroadlyRequest) (*pb.SearchBroadlyResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, deadlineInteractive)
	defer cancel()

	resp := &pb.SearchBroadlyResponse{
		OverallStatus: "ok",
		Completeness:  "full",
	}

	max := int(req.MaxResults)
	if max <= 0 {
		max = 20
	}
	if max > 100 {
		max = 100
	}

	mode := req.Mode
	if mode == "" {
		mode = "auto"
	}
	detail := req.Detail
	if detail == "" {
		detail = "standard"
	}

	// Hydration hint: request chunks up front to avoid a second Batch
	hint := &pb.HydrationHint{Chunks: true, Metadata: true, Provenance: true, ChunksPerNode: 2}

	type srcResult struct {
		name    string
		results []*pb.ScoredNode
		status  *pb.SourceStatus
	}
	ch := make(chan srcResult, 3)
	started := 0

	if mode == "auto" || mode == "lexical" {
		started++
		go func() {
			r, err := h.FTS.Search(ctx, &pb.FTSSearchRequest{
				Query: req.Query, K: int32(max), ContentClasses: req.ContentClasses, Hydrate: hint,
			})
			if err != nil || r == nil {
				ch <- srcResult{"fts", nil, &pb.SourceStatus{
					SourceName: "fts", Status: "DEGRADED", ErrorMessage: "fts source unavailable",
				}}
				return
			}
			ch <- srcResult{"fts", r.Results, r.Status}
		}()
	}
	if mode == "auto" || mode == "filename" {
		started++
		go func() {
			r, err := h.Filename.Search(ctx, &pb.FilenameSearchRequest{
				Query: req.Query, K: int32(max / 2), Hydrate: hint,
			})
			if err != nil || r == nil {
				ch <- srcResult{"filename", nil, &pb.SourceStatus{
					SourceName: "filename", Status: "DEGRADED", ErrorMessage: "filename source unavailable",
				}}
				return
			}
			ch <- srcResult{"filename", r.Results, r.Status}
		}()
	}
	if mode == "auto" || mode == "semantic" {
		if h.Embedder != nil {
			started++
			go func() {
				vec, err := h.Embedder.EmbedQuery(req.Query)
				if err != nil {
					ch <- srcResult{"vector", nil, &pb.SourceStatus{
						SourceName: "vector", Status: "DEGRADED", ErrorMessage: "embedding failed",
					}}
					return
				}
				r, err := h.Vector.Search(ctx, &pb.VectorSearchRequest{
					QueryEmbeddingInt8: vec, K: int32(max), ContentClasses: req.ContentClasses, Hydrate: hint,
				})
				if err != nil || r == nil {
					ch <- srcResult{"vector", nil, &pb.SourceStatus{
						SourceName: "vector", Status: "DEGRADED", ErrorMessage: "vector source unavailable",
					}}
					return
				}
				ch <- srcResult{"vector", r.Results, r.Status}
			}()
		} else if mode == "semantic" {
			// Required source missing: hard error per spec §9.5
			resp.OverallStatus = "error"
			resp.Completeness = "partial"
			resp.FailedSources = []string{"vector"}
			return resp, nil
		}
	}

	rankedLists := make(map[string][]*pb.ScoredNode)
	for i := 0; i < started; i++ {
		select {
		case r := <-ch:
			resp.Sources = append(resp.Sources, r.status)
			if r.status != nil && r.status.Status != "OK" && r.status.Status != "EMPTY_BUT_EXECUTED" {
				resp.FailedSources = append(resp.FailedSources, r.name)
			}
			if len(r.results) > 0 {
				// Layer 3 EGRESS redaction (D17): full Sanitizer (patterns +
				// long-token heuristic) at this single choke point so every hit's
				// chunk text — and the snippet shapeHits derives from it — is
				// redacted before it leaves the daemon, covering pre-Phase-7
				// indexes whose chunks were never ingest-redacted.
				redactScoredNodes(r.results)
				rankedLists[r.name] = r.results
			}
		case <-ctx.Done():
			resp.Warnings = append(resp.Warnings, "deadline exceeded")
			resp.Completeness = "partial"
			resp.OverallStatus = "degraded"
		}
	}

	// Required-source enforcement per spec §9.5.
	// A source that executed but returned no results (EMPTY_BUT_EXECUTED)
	// is still a successful execution — only truly failed sources trigger error.
	if mode == "lexical" {
		if !sourceExecutedOK(resp.Sources, "fts") {
			resp.OverallStatus = "error"
			return resp, nil
		}
	}
	if mode == "filename" {
		if !sourceExecutedOK(resp.Sources, "filename") {
			resp.OverallStatus = "error"
			return resp, nil
		}
	}
	if mode == "semantic" {
		if !sourceExecutedOK(resp.Sources, "vector") {
			resp.OverallStatus = "error"
			return resp, nil
		}
	}

	// RRF fusion
	fused := rrfFuse(rankedLists, rrfK)
	if len(fused) > max {
		fused = fused[:max]
	}

	// Shape to SearchHit with detail-level trimming
	resp.Results = shapeHits(fused, detail)

	if len(resp.FailedSources) > 0 {
		resp.Completeness = "partial"
		if resp.OverallStatus == "ok" {
			resp.OverallStatus = "degraded"
		}
		resp.Retryable = true
	}

	return resp, nil
}

func containsSource(sources []*pb.SourceStatus, name, status string) bool {
	for _, s := range sources {
		if s != nil && s.SourceName == name && s.Status == status {
			return true
		}
	}
	return false
}

// sourceExecutedOK returns true if the named source executed successfully.
// Both "OK" and "EMPTY_BUT_EXECUTED" count as successful execution.
func sourceExecutedOK(sources []*pb.SourceStatus, name string) bool {
	for _, s := range sources {
		if s != nil && s.SourceName == name && (s.Status == "OK" || s.Status == "EMPTY_BUT_EXECUTED") {
			return true
		}
	}
	return false
}

type fusedHit struct {
	nodeID         int64
	hydrated       *pb.HydratedNode
	score          float64
	matchedSources []string
}

// rrfFuse implements Reciprocal Rank Fusion with constant k.
// Per spec §10.3, ties break on node_id ascending — no diversity bonus.
func rrfFuse(lists map[string][]*pb.ScoredNode, k int) []fusedHit {
	scores := make(map[int64]*fusedHit)
	for srcName, list := range lists {
		for rank, node := range list {
			h, ok := scores[node.NodeId]
			if !ok {
				h = &fusedHit{nodeID: node.NodeId, hydrated: node.Hydrated}
				scores[node.NodeId] = h
			}
			h.score += 1.0 / float64(k+rank+1)
			h.matchedSources = append(h.matchedSources, srcName)
			if h.hydrated == nil && node.Hydrated != nil {
				h.hydrated = node.Hydrated
			}
		}
	}
	out := make([]fusedHit, 0, len(scores))
	for _, h := range scores {
		out = append(out, *h)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].nodeID < out[j].nodeID // tie-break on node_id asc per §10.3
	})
	return out
}

// shapeHits maps fused results to the wire SearchHit with detail-level text shaping.
// brief:    title + 1-line snippet (120 chars)
// standard: title + snippet + path (400 chars)
// full:     everything including all chunks
func shapeHits(fused []fusedHit, detail string) []*pb.SearchHit {
	out := make([]*pb.SearchHit, 0, len(fused))
	for _, f := range fused {
		h := &pb.SearchHit{
			NodeId:         f.nodeID,
			Score:          float32(f.score),
			MatchedSources: f.matchedSources,
		}
		if f.hydrated != nil {
			h.Path = f.hydrated.Path
			h.Title = f.hydrated.Name
			h.Snippet = firstSnippet(f.hydrated, detail)
		}
		out = append(out, h)
	}
	return out
}

func firstSnippet(n *pb.HydratedNode, detail string) string {
	if len(n.Chunks) == 0 {
		return ""
	}
	text := n.Chunks[0].Text
	switch detail {
	case "brief":
		return truncate(text, 120)
	case "full":
		var parts []string
		for _, c := range n.Chunks {
			parts = append(parts, c.Text)
		}
		return strings.Join(parts, "\n---\n")
	default: // standard
		return truncate(text, 400)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
