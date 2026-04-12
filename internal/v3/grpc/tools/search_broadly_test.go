//go:build cgo

package tools

import (
	"testing"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
)

func TestRRFFuseTieBreakOnNodeID(t *testing.T) {
	lists := map[string][]*pb.ScoredNode{
		"fts": {
			{NodeId: 10, Score: 0.9},
			{NodeId: 20, Score: 0.8},
		},
		"filename": {
			{NodeId: 20, Score: 0.95},
			{NodeId: 10, Score: 0.85},
		},
	}
	fused := rrfFuse(lists, 60)
	if len(fused) != 2 {
		t.Fatalf("fused = %d, want 2", len(fused))
	}
	// Both appear in both lists at rank 0 or 1 → same RRF score → tie-break on node_id asc
	if fused[0].nodeID != 10 {
		t.Errorf("top = %d, want 10 (tie-break)", fused[0].nodeID)
	}
}

func TestRRFFuseSingleSource(t *testing.T) {
	lists := map[string][]*pb.ScoredNode{
		"fts": {
			{NodeId: 1, Score: 0.9},
			{NodeId: 2, Score: 0.5},
			{NodeId: 3, Score: 0.3},
		},
	}
	fused := rrfFuse(lists, 60)
	if len(fused) != 3 {
		t.Fatalf("fused = %d, want 3", len(fused))
	}
	// Rank 0 gets highest RRF score
	if fused[0].nodeID != 1 {
		t.Errorf("top = %d, want 1", fused[0].nodeID)
	}
	// Each node should have one matched source
	if len(fused[0].matchedSources) != 1 || fused[0].matchedSources[0] != "fts" {
		t.Errorf("matchedSources = %v, want [fts]", fused[0].matchedSources)
	}
}

func TestRRFFuseMultiSourceBoostsScore(t *testing.T) {
	lists := map[string][]*pb.ScoredNode{
		"fts":      {{NodeId: 1, Score: 0.9}},
		"filename": {{NodeId: 2, Score: 0.9}},
		"vector":   {{NodeId: 1, Score: 0.8}},
	}
	fused := rrfFuse(lists, 60)
	// Node 1 appears in 2 sources, node 2 in 1 → node 1 should rank higher
	if fused[0].nodeID != 1 {
		t.Errorf("top = %d, want 1 (multi-source boost)", fused[0].nodeID)
	}
	if len(fused[0].matchedSources) != 2 {
		t.Errorf("matchedSources len = %d, want 2", len(fused[0].matchedSources))
	}
}

func TestShapeHitsBriefTruncates(t *testing.T) {
	n := &pb.HydratedNode{
		Name: "x", Path: "/a",
		Chunks: []*pb.HydratedChunk{
			{Text: "lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua, enim ad minim veniam quis nostrud exercitation ullamco"},
		},
	}
	hits := shapeHits([]fusedHit{{nodeID: 1, hydrated: n, score: 1.0}}, "brief")
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}
	// 120 chars + "…" = max 123 bytes for ASCII
	if len(hits[0].Snippet) > 125 {
		t.Errorf("brief snippet not truncated: len=%d", len(hits[0].Snippet))
	}
}

func TestShapeHitsStandard(t *testing.T) {
	n := &pb.HydratedNode{
		Name: "test.go", Path: "/src/test.go",
		Chunks: []*pb.HydratedChunk{
			{Text: "short text"},
		},
	}
	hits := shapeHits([]fusedHit{{nodeID: 1, hydrated: n, score: 0.5}}, "standard")
	if hits[0].Title != "test.go" {
		t.Errorf("title = %s, want test.go", hits[0].Title)
	}
	if hits[0].Path != "/src/test.go" {
		t.Errorf("path = %s, want /src/test.go", hits[0].Path)
	}
	if hits[0].Snippet != "short text" {
		t.Errorf("snippet = %s, want short text", hits[0].Snippet)
	}
}

func TestShapeHitsFullJoinsChunks(t *testing.T) {
	n := &pb.HydratedNode{
		Name: "x", Path: "/a",
		Chunks: []*pb.HydratedChunk{
			{Text: "chunk one"},
			{Text: "chunk two"},
		},
	}
	hits := shapeHits([]fusedHit{{nodeID: 1, hydrated: n, score: 1.0}}, "full")
	want := "chunk one\n---\nchunk two"
	if hits[0].Snippet != want {
		t.Errorf("full snippet = %q, want %q", hits[0].Snippet, want)
	}
}

func TestShapeHitsNoHydratedNode(t *testing.T) {
	hits := shapeHits([]fusedHit{{nodeID: 42, hydrated: nil, score: 0.5}}, "standard")
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}
	if hits[0].NodeId != 42 {
		t.Errorf("nodeId = %d, want 42", hits[0].NodeId)
	}
	if hits[0].Snippet != "" {
		t.Errorf("snippet should be empty for nil hydrated, got %q", hits[0].Snippet)
	}
}

func TestContainsSource(t *testing.T) {
	sources := []*pb.SourceStatus{
		{SourceName: "fts", Status: "OK"},
		{SourceName: "filename", Status: "DEGRADED"},
	}
	if !containsSource(sources, "fts", "OK") {
		t.Error("should find fts OK")
	}
	if containsSource(sources, "filename", "OK") {
		t.Error("filename is DEGRADED, not OK")
	}
	if containsSource(sources, "vector", "OK") {
		t.Error("vector not in sources")
	}
}

func TestSourceExecutedOK(t *testing.T) {
	sources := []*pb.SourceStatus{
		{SourceName: "fts", Status: "OK"},
		{SourceName: "filename", Status: "EMPTY_BUT_EXECUTED"},
		{SourceName: "vector", Status: "DEGRADED"},
	}
	if !sourceExecutedOK(sources, "fts") {
		t.Error("fts OK should count as executed")
	}
	if !sourceExecutedOK(sources, "filename") {
		t.Error("filename EMPTY_BUT_EXECUTED should count as executed")
	}
	if sourceExecutedOK(sources, "vector") {
		t.Error("vector DEGRADED should NOT count as executed")
	}
	if sourceExecutedOK(sources, "missing") {
		t.Error("missing source should NOT count as executed")
	}
}
