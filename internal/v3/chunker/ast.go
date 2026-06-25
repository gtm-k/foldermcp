//go:build cgo

package chunker

import (
	"github.com/gtm-k/foldermcp/internal/v3/grammar"
)

// CodeChunk represents a chunk of code tied to a specific AST symbol.
type CodeChunk struct {
	Symbol     grammar.Symbol
	Text       string
	ByteStart  int
	ByteEnd    int
	TokenCount int
}

// ChunkCode emits one chunk per extracted symbol, with the chunk text
// being the exact source slice for that symbol. Symbols larger than
// MaxTokens fall through to the prose chunker on their source slice
// (so a 2000-line function is still retrievable as multiple chunks).
func ChunkCode(source []byte, syms []grammar.Symbol, cfg Config, counter Counter) []CodeChunk {
	var out []CodeChunk
	for _, s := range syms {
		body := string(source[s.ByteStart:s.ByteEnd])
		toks := counter.Count(body)
		if toks <= cfg.MaxTokens {
			out = append(out, CodeChunk{
				Symbol: s, Text: body,
				ByteStart: int(s.ByteStart), ByteEnd: int(s.ByteEnd),
				TokenCount: toks,
			})
			continue
		}
		// Oversize: fall back to prose chunker on the body
		subs := ChunkProse(body, cfg, counter)
		for _, sc := range subs {
			out = append(out, CodeChunk{
				Symbol: s, Text: sc.Text,
				ByteStart: int(s.ByteStart) + sc.ByteStart,
				ByteEnd:   int(s.ByteStart) + sc.ByteEnd,
				TokenCount: sc.TokenCount,
			})
		}
	}
	return out
}
