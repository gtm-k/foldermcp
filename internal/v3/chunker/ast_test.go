//go:build cgo

package chunker

import (
	"testing"

	"github.com/gtm-k/foldermcp/internal/v3/grammar"
)

func TestChunkCodeOnePerSymbol(t *testing.T) {
	src := []byte("package x\nfunc Foo() int { return 1 }\nfunc Bar() int { return 2 }\n")
	ext, err := grammar.NewGoExtractor()
	if err != nil {
		t.Fatalf("NewGoExtractor: %v", err)
	}
	syms, err := ext.Extract(src)
	if err != nil {
		t.Fatal(err)
	}
	chunks := ChunkCode(src, syms, DefaultConfig(), wordCounter{})
	if len(chunks) != 2 {
		t.Errorf("got %d chunks, want 2", len(chunks))
	}
	for _, c := range chunks {
		if c.Text == "" {
			t.Errorf("empty chunk for symbol %s", c.Symbol.Name)
		}
	}
}

func TestChunkCodeOversizeFallsBack(t *testing.T) {
	// Create a fake oversized symbol
	body := make([]byte, 300)
	for i := range body {
		body[i] = 'a' + byte(i%26)
		if i%5 == 4 {
			body[i] = ' '
		}
	}
	src := body
	sym := grammar.Symbol{
		Kind: "function", Name: "Big",
		ByteStart: 0, ByteEnd: uint(len(src)),
	}
	cfg := Config{TargetTokens: 10, MinTokens: 3, MaxTokens: 15, OverlapToks: 0}
	chunks := ChunkCode(src, []grammar.Symbol{sym}, cfg, wordCounter{})
	if len(chunks) <= 1 {
		t.Errorf("oversized symbol should produce multiple chunks, got %d", len(chunks))
	}
	for _, c := range chunks {
		if c.Symbol.Name != "Big" {
			t.Errorf("chunk symbol = %q, want Big", c.Symbol.Name)
		}
	}
}
