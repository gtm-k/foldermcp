//go:build cgo

package query

import (
	"encoding/json"
	"log/slog"
	"strings"
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
		noteEgressRedaction(idKind, id)
	}
	return out
}

// noteEgressRedaction records that an egress pass actually masked a secret and
// emits the rate-limited WARN (MED-6). Shared by redactEgressText and the
// structural PropertiesJson path so both keep identical observability.
func noteEgressRedaction(idKind string, id int64) {
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

// redactPropertiesJSON redacts secrets from a node's PropertiesJson STRUCTURALLY
// rather than as flat text (F1). PropertiesJson is a JSON STRING; running the
// flat egress Sanitizer over the whole string lets its greedy patterns
// (api[_-]?key…\S{10,}, sk-…{20,}) consume the closing quote and brace, yielding
// INVALID JSON that breaks any consumer doing json.Unmarshal on properties_json.
//
// Instead we json.Unmarshal into a generic structure, recursively sanitize every
// STRING value (and map KEYS — a secret could be a key) with the egress
// Sanitizer, then re-marshal. Result is ALWAYS valid JSON AND secret-free.
//
// Fallback: empty/whitespace input passes through unchanged; if Unmarshal fails
// (the stored properties is not valid JSON) we do NOT emit the corrupt-but-
// flat-redacted string — we return a valid empty object "{}" so the egress
// contract (valid JSON, no leaked secret) holds even on malformed storage.
func redactPropertiesJSON(raw string, idKind string, id int64) string {
	if strings.TrimSpace(raw) == "" {
		return raw
	}
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		// Malformed stored properties: never emit corrupt output, never risk
		// leaking an unparsed secret. Treat as an egress catch for observability.
		noteEgressRedaction(idKind, id)
		return "{}"
	}
	changed := false
	v = sanitizeJSONValue(v, &changed)
	if !changed {
		return raw
	}
	noteEgressRedaction(idKind, id)
	out, err := json.Marshal(v)
	if err != nil {
		// Re-marshal of a value that was just unmarshalled should not fail; if it
		// somehow does, fall back to a valid empty object rather than the raw.
		return "{}"
	}
	return string(out)
}

// sanitizeJSONValue recursively applies the egress Sanitizer to every string
// VALUE and map KEY in a decoded JSON value, setting *changed if anything was
// masked. Numbers, bools and null pass through untouched.
func sanitizeJSONValue(v any, changed *bool) any {
	switch t := v.(type) {
	case string:
		s := egressSanitizer.Sanitize(t)
		if s != t {
			*changed = true
		}
		return s
	case []any:
		for i, e := range t {
			t[i] = sanitizeJSONValue(e, changed)
		}
		return t
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			nk := egressSanitizer.Sanitize(k)
			if nk != k {
				*changed = true
			}
			out[nk] = sanitizeJSONValue(e, changed)
		}
		return out
	default:
		return v
	}
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
// signature, e.g. `const apiKey = "..."`) and any hydrated chunk text.
// PropertiesJson is redacted STRUCTURALLY (redactPropertiesJSON) so the egressed
// value stays valid JSON; chunk text is flat text and uses the flat Sanitizer.
// Nil-safe.
func redactHydratedNodeFields(n *pb.HydratedNode) {
	if n == nil {
		return
	}
	n.PropertiesJson = redactPropertiesJSON(n.PropertiesJson, "node_id", n.NodeId)
	for _, c := range n.Chunks {
		redactHydratedChunk(c)
	}
}
