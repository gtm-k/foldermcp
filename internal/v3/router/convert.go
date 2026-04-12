package router

import (
	"strings"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
)

// ── Proto → JSON conversion helpers ────────────────────────

// sanitizeSourceError replaces raw internal error messages with generic
// descriptions safe for MCP clients. Prevents leaking SQL, embedder, or
// filesystem internals through per-source error_message fields.
func sanitizeSourceError(msg string) string {
	if msg == "" {
		return ""
	}
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "sql") || strings.Contains(lower, "sqlite"):
		return "database query failed"
	case strings.Contains(lower, "embed"):
		return "embedding operation failed"
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline"):
		return "operation timed out"
	case strings.Contains(lower, "connect") || strings.Contains(lower, "refused"):
		return "service connection failed"
	default:
		return "internal error"
	}
}

func searchResultFromProto(p *pb.SearchBroadlyResponse) *SearchResult {
	if p == nil {
		return &SearchResult{Status: "error", Completeness: "partial"}
	}
	out := &SearchResult{
		Status:        p.OverallStatus,
		Completeness:  p.Completeness,
		FailedSources: p.FailedSources,
		Retryable:     p.Retryable,
		Warnings:      p.Warnings,
	}
	for _, s := range p.Sources {
		if s == nil {
			continue
		}
		out.Sources = append(out.Sources, SourceInfo{
			SourceName:   s.SourceName,
			Status:       s.Status,
			LatencyMs:    s.LatencyMs,
			ErrorMessage: sanitizeSourceError(s.ErrorMessage),
		})
	}
	for _, h := range p.Results {
		if h == nil {
			continue
		}
		out.Results = append(out.Results, SearchHitJSON{
			NodeID:         h.NodeId,
			Path:           h.Path,
			Title:          h.Title,
			Snippet:        h.Snippet,
			Score:          h.Score,
			MatchedSources: h.MatchedSources,
		})
	}
	return out
}

func inspectResultFromProto(p *pb.InspectNodeResponse) *InspectResult {
	if p == nil {
		return &InspectResult{Status: "error"}
	}
	r := &InspectResult{}
	if p.Status != nil {
		r.Status = p.Status.Status
	}
	if p.Node == nil {
		return r
	}
	r.Node = &NodeJSON{
		NodeID:       p.Node.NodeId,
		Path:         p.Node.Path,
		Name:         p.Node.Name,
		NodeType:     p.Node.NodeType,
		Provenance:   p.Node.Provenance,
		ContentClass: p.Node.ContentClass,
		Language:     p.Node.Language,
	}
	for _, c := range p.Node.Chunks {
		if c == nil {
			continue
		}
		r.Node.Chunks = append(r.Node.Chunks, ChunkJSON{
			ChunkID:    c.ChunkId,
			Text:       c.Text,
			TokenCount: c.TokenCount,
		})
	}
	return r
}

func browseResultFromProto(p *pb.BrowseFolderResponse) *BrowseResult {
	if p == nil {
		return &BrowseResult{Status: "error"}
	}
	out := &BrowseResult{NextCursor: p.NextCursor}
	if p.Status != nil {
		out.Status = p.Status.Status
	}
	for _, e := range p.Entries {
		if e == nil {
			continue
		}
		out.Entries = append(out.Entries, FolderEntryJSON{
			Name:         e.Name,
			Path:         e.Path,
			IsDir:        e.IsDir,
			Size:         e.Size,
			Mtime:        e.Mtime,
			ContentClass: e.ContentClass,
			FileID:       e.FileId,
		})
	}
	return out
}
