//go:build cgo

package embed

import (
	"database/sql"

	"github.com/gtm-k/foldermcp/internal/v3/store"
)

// SemanticIndexCompatible reports whether the store's persisted embeddings were
// quantized with the same scheme this binary's query path produces, so that
// semantic (vector) search compares like-for-like int8 codes.
//
// It returns ok=true when the fingerprint is absent (a fresh index the indexer
// will populate) OR its quantization_mode equals QuantizationModeString(). It
// returns ok=false with the stored mode when a fingerprint row exists with a
// DIFFERENT mode: serving semantic search then compares this binary's
// fixed-scale query codes against differently-scaled stored codes (e.g. an old
// per-vector "int8" index, or a fixed index built at a different Int8Scale) on
// a common sqlite-vec L2 metric — which silently returns garbage rankings with
// no error to any actor. The read paths (serve-v3, all-v3) use this to disable
// semantic search and degrade to lexical, surfacing the reason on stderr.
//
// The write path's EnsureFingerprint remains the authoritative guard that
// halts an indexer on mismatch; this is the read-side mirror, because the
// indexer and query server can run as separate processes against one DB.
func SemanticIndexCompatible(db *sql.DB) (ok bool, storedMode string) {
	fp, err := store.ReadFingerprint(db)
	if err != nil {
		// No row (fresh index) or a transient read error: do not block
		// semantic search. A fresh index is compatible by construction once
		// the indexer writes the fingerprint.
		return true, ""
	}
	if fp.QuantizationMode != QuantizationModeString() {
		return false, fp.QuantizationMode
	}
	return true, ""
}
