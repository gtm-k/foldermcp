package router

import (
	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
)

// ── Proto → JSON conversion helpers ────────────────────────

func searchResultFromProto(p *pb.SearchBroadlyResponse) *SearchResult {
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
			ErrorMessage: s.ErrorMessage,
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
