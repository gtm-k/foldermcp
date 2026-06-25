//go:build cgo

package embed

import "fmt"

// EmbedQuery tokenizes text, runs the ONNX model for batch=1,
// mean-pools + L2-normalizes, quantizes to int8, and returns 384 bytes.
// Called by SearchBroadly (Phase E), the retrieval harness (Phase G),
// and per chunk by the pipeline Runner (D28b) — the tokenizer is cached
// on the Embedder, so repeated calls do not reload tokenizer.json.
//
// Safe for concurrent use: e.mu serializes tokenize+infer because the
// underlying Embed() writes into shared pre-allocated ORT tensors
// (E2 review finding 1 — concurrent gRPC search handlers share one
// query Embedder).
func (e *Embedder) EmbedQuery(text string) ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	tok, err := e.loadTokenizer()
	if err != nil {
		return nil, err
	}
	ids, mask, tts := tok.Encode(text)

	pooled, err := e.Embed(ids, mask, tts)
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	if len(pooled) != Dimension {
		return nil, fmt.Errorf("unexpected output dimension: %d", len(pooled))
	}
	quant := QuantizeInt8(pooled)
	blob := make([]byte, Dimension)
	for i, v := range quant {
		blob[i] = byte(v)
	}
	return blob, nil
}
