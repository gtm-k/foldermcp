//go:build cgo

package embed

import "github.com/gtm-k/foldermcp/internal/v3/embed/berttok"

// Tokenizer is the embed package's BERT WordPiece tokenizer. It is a thin
// wrapper over the pure-Go, HF-parity berttok implementation (which is cgo-free
// and unit-tested to byte-for-byte parity with the real HuggingFace tokenizer
// in internal/v3/embed/berttok). Encode returns (input_ids, attention_mask,
// token_type_ids), each of length MaxSeqLen.
//
// Replaces the M1 placeholder tokenizer (whitespace-split, no punctuation
// pre-tokenization) that produced wrong word-pieces and crippled semantic
// retrieval (recall@10 0.12 -> ~0.39 after this fix).
type Tokenizer struct {
	inner *berttok.Tokenizer
}

// LoadTokenizer loads a HuggingFace tokenizer.json and returns a tokenizer that
// pads/truncates to MaxSeqLen.
func LoadTokenizer(path string) (*Tokenizer, error) {
	inner, err := berttok.New(path, MaxSeqLen)
	if err != nil {
		return nil, err
	}
	return &Tokenizer{inner: inner}, nil
}

// Encode returns (input_ids, attention_mask, token_type_ids) each of length
// MaxSeqLen, matching the HuggingFace BERT tokenizer.
func (t *Tokenizer) Encode(text string) ([]int64, []int64, []int64) {
	return t.inner.Encode(text)
}
