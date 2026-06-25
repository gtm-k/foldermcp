//go:build cgo

package query

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gtm-k/foldermcp/internal/sandbox"
	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
)

// egressSanitizer is the SHARED EGRESS redaction choke point for the query layer
// (MED-3). Relocating egress redaction here — into the hydration helpers that
// build every HydratedChunk.Text and HydratedNode — means EVERY egress path
// inherits it: not just the tools-layer SearchBroadly/Inspect, but the
// lower-level IndexQuery RPCs (GetChunks/GetNodes) and Batch, which previously
// returned RAW text. A local same-user client could otherwise retrieve
// un-redacted secrets that were indexed before Phase 7 shipped (whose chunk text
// was never ingest-redacted) or that an ingest pattern missed.
//
// It runs the FULL Sanitizer.Sanitize — the frozen sandbox patterns PLUS the
// 40+-char long-token heuristic — because the long-token rule is acceptable at
// DISPLAY time (it would be too destructive at ingest, where it would shred git
// SHAs / hashes / base64 in the index, per D17, but at egress it is pure
// defense-in-depth). maxBytes=0 ⇒ no truncation here; the tools layer does its
// own detail-level shaping. Redaction is idempotent, so the tools layer running
// Sanitize a second time on already-clean text is harmless.
var egressSanitizer = sandbox.NewSanitizer(0)

// egressRedactionCount counts egress passes that ACTUALLY changed text — i.e. an
// unredacted secret reached the index (an ingest miss or a pre-Phase-7 index) and
// egress caught it (MED-6). Exported via the rate-limited WARN below.
var egressRedactionCount atomic.Int64

var (
	egressWarnMu   sync.Mutex
	egressWarnLast time.Time
)

// egressWarnInterval rate-limits the "egress caught a secret" WARN so a corpus
// full of pre-Phase-7 secrets cannot flood the log on every query.
const egressWarnInterval = 30 * time.Second

// redactEgressText runs the egress sanitizer over a single field and, if it
// actually changed (a secret slipped past ingest), increments the egress counter
// and emits a rate-limited WARN tagged with the node/chunk id (NEVER the secret)
// so the operator can observe that egress is catching what ingest missed (MED-6).
func redactEgressText(in string, idKind string, id int64) string {
	out := egressSanitizer.Sanitize(in)
	if out != in {
		egressRedactionCount.Add(1)
		egressWarnMu.Lock()
		now := time.Now()
		if now.Sub(egressWarnLast) >= egressWarnInterval {
			egressWarnLast = now
			egressWarnMu.Unlock()
			slog.Default().Warn("query: egress redaction fired — an unredacted secret reached the index (ingest miss or pre-Phase-7 index); display path masked it",
				idKind, id, "egress_redactions_total", egressRedactionCount.Load())
		} else {
			egressWarnMu.Unlock()
		}
	}
	return out
}

// redactHydratedChunk sanitizes a chunk's Text in place at the shared hydration
// choke point. Nil-safe.
func redactHydratedChunk(c *pb.HydratedChunk) {
	if c == nil {
		return
	}
	c.Text = redactEgressText(c.Text, "chunk_id", c.ChunkId)
}

// redactHydratedNodeFields sanitizes the free-text fields a hydrated node carries
// to the client: PropertiesJson (which can carry a secret in a Go symbol
// signature, e.g. `const apiKey = "..."`) and any hydrated chunk text. Nil-safe.
func redactHydratedNodeFields(n *pb.HydratedNode) {
	if n == nil {
		return
	}
	n.PropertiesJson = redactEgressText(n.PropertiesJson, "node_id", n.NodeId)
	for _, c := range n.Chunks {
		redactHydratedChunk(c)
	}
}
