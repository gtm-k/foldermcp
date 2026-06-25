//go:build cgo

package tools

import (
	"strings"
	"testing"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
)

// Phase 7 Layer 3 (D17) — EGRESS redaction at the grpc/tools choke point. A
// constructed tool response carrying a secret in its chunk text is redacted
// before it leaves the daemon. Egress runs the FULL Sanitizer (patterns +
// long-token heuristic), so it is the defense-in-depth net over indexes built
// before Phase 7.

const (
	egAWS = "AKIAIOSFODNN7EXAMPLE"
	egGH  = "ghp_0123456789abcdefghijABCDEFGHIJ012345"
)

func TestEgress_RedactHydratedNode(t *testing.T) {
	n := &pb.HydratedNode{
		NodeId: 7,
		Chunks: []*pb.HydratedChunk{
			{ChunkId: 1, Text: "deploy with key " + egAWS + " now"},
			{ChunkId: 2, Text: "token=" + egGH},
			{ChunkId: 3, Text: "no secret here, just prose"},
			nil, // nil-safe
		},
	}
	redactHydratedNode(n)

	if strings.Contains(n.Chunks[0].Text, egAWS) {
		t.Errorf("AWS key survived egress: %q", n.Chunks[0].Text)
	}
	if !strings.Contains(n.Chunks[0].Text, "[REDACTED]") {
		t.Errorf("no [REDACTED] marker in chunk 0: %q", n.Chunks[0].Text)
	}
	if strings.Contains(n.Chunks[1].Text, egGH) {
		t.Errorf("GitHub token survived egress: %q", n.Chunks[1].Text)
	}
	if n.Chunks[2].Text != "no secret here, just prose" {
		t.Errorf("clean chunk altered: %q", n.Chunks[2].Text)
	}
}

// TestEgress_LongTokenHeuristicActiveAtDisplay confirms the egress layer DOES
// run the 40+-char long-token heuristic (acceptable at display time, D17),
// catching tokens the pattern-only ingest layer would leave — defense-in-depth
// for pre-Phase-7 indexes.
func TestEgress_LongTokenHeuristicActiveAtDisplay(t *testing.T) {
	longToken := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789" // 62 chars
	n := &pb.HydratedNode{Chunks: []*pb.HydratedChunk{{ChunkId: 1, Text: "leftover " + longToken}}}
	redactHydratedNode(n)
	if strings.Contains(n.Chunks[0].Text, longToken) {
		t.Errorf("long token survived egress (heuristic must run at display): %q", n.Chunks[0].Text)
	}
	if !strings.Contains(n.Chunks[0].Text, "[REDACTED]") {
		t.Errorf("no [REDACTED] marker: %q", n.Chunks[0].Text)
	}
}

func TestEgress_RedactScoredNodes(t *testing.T) {
	nodes := []*pb.ScoredNode{
		{NodeId: 1, Hydrated: &pb.HydratedNode{Chunks: []*pb.HydratedChunk{{Text: "key " + egAWS}}}},
		nil,                        // nil-safe
		{NodeId: 2, Hydrated: nil}, // nil hydrated-safe
	}
	redactScoredNodes(nodes)
	if strings.Contains(nodes[0].Hydrated.Chunks[0].Text, egAWS) {
		t.Errorf("AWS key survived egress in scored node: %q", nodes[0].Hydrated.Chunks[0].Text)
	}
}

func TestEgress_NilSafe(t *testing.T) {
	redactHydratedNode(nil) // must not panic
	redactScoredNodes(nil)  // must not panic
	redactScoredNodes([]*pb.ScoredNode{nil})
}
