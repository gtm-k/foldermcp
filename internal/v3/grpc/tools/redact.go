//go:build cgo

package tools

import (
	"github.com/gtm-k/foldermcp/internal/sandbox"
	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
)

// egressSanitizer is the single EGRESS redaction choke point for the tools
// layer (Phase 7 Layer 3, D17). Unlike ingest (pattern-only, runner.go), egress
// runs the FULL Sanitizer.Sanitize — patterns PLUS the 40+-char long-token
// heuristic — because the long-token rule is acceptable at DISPLAY time and
// provides defense-in-depth over indexes built before Phase 7 (whose chunk text
// was never ingest-redacted) and over future pattern updates. maxBytes=0 ⇒ no
// truncation here (the tools layer does its own detail-level shaping).
var egressSanitizer = sandbox.NewSanitizer(0)

// redactHydratedNode redacts secrets from a hydrated node's chunk text in place
// at the result-shaping choke point, so BOTH MCP and CLI consumers (which share
// these handlers) inherit it. Chunk text is the only free-text field a hydrated
// node carries to the client; the snippet that SearchBroadly derives is built
// from this same redacted chunk text, so redacting here covers both the
// full-text (Inspect) and snippet (Search) egress paths. Nil-safe.
func redactHydratedNode(n *pb.HydratedNode) {
	if n == nil {
		return
	}
	for _, c := range n.Chunks {
		if c == nil {
			continue
		}
		c.Text = egressSanitizer.Sanitize(c.Text)
	}
}

// redactScoredNodes applies redactHydratedNode to every scored node's hydrated
// payload (the form SearchBroadly fuses before snippet shaping).
func redactScoredNodes(nodes []*pb.ScoredNode) {
	for _, sn := range nodes {
		if sn != nil {
			redactHydratedNode(sn.Hydrated)
		}
	}
}
