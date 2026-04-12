//go:build cgo

package chunker

import (
	"strings"
	"testing"
)

type wordCounter struct{}

func (wordCounter) Count(s string) int {
	if strings.TrimSpace(s) == "" {
		return 0
	}
	return len(strings.Fields(s))
}

func TestChunkProseRespectsMaxTokens(t *testing.T) {
	cfg := Config{TargetTokens: 10, MinTokens: 3, MaxTokens: 15, OverlapToks: 0}
	// 60 words
	words := make([]string, 60)
	for i := range words {
		words[i] = "word"
	}
	text := strings.Join(words, " ")

	chunks := ChunkProse(text, cfg, wordCounter{})
	if len(chunks) == 0 {
		t.Fatal("no chunks produced")
	}
	for _, ch := range chunks {
		if ch.TokenCount > cfg.MaxTokens {
			t.Errorf("chunk has %d tokens, max %d", ch.TokenCount, cfg.MaxTokens)
		}
	}
}

func TestChunkProseShortText(t *testing.T) {
	cfg := DefaultConfig()
	chunks := ChunkProse("hello world", cfg, wordCounter{})
	if len(chunks) != 1 {
		t.Fatalf("short text → %d chunks, want 1", len(chunks))
	}
}

func TestChunkProseEmptyText(t *testing.T) {
	cfg := DefaultConfig()
	chunks := ChunkProse("", cfg, wordCounter{})
	if len(chunks) != 0 {
		t.Errorf("empty text → %d chunks, want 0", len(chunks))
	}
}

func TestChunkProseParagraphBoundaries(t *testing.T) {
	// Two paragraphs, each under max tokens
	cfg := Config{TargetTokens: 10, MinTokens: 3, MaxTokens: 15, OverlapToks: 0}
	text := "one two three four five\n\nsix seven eight nine ten"
	chunks := ChunkProse(text, cfg, wordCounter{})
	if len(chunks) != 2 {
		t.Errorf("paragraph split → %d chunks, want 2", len(chunks))
	}
}

func TestChunkProseByteOffsetsSpanInput(t *testing.T) {
	cfg := Config{TargetTokens: 5, MinTokens: 2, MaxTokens: 8, OverlapToks: 0}
	text := "a b c d e f g h i j k l m n o"
	chunks := ChunkProse(text, cfg, wordCounter{})
	if len(chunks) == 0 {
		t.Fatal("no chunks")
	}
	// First chunk starts at 0
	if chunks[0].ByteStart != 0 {
		t.Errorf("first chunk ByteStart = %d, want 0", chunks[0].ByteStart)
	}
	// Last chunk ends at len(text)
	last := chunks[len(chunks)-1]
	if last.ByteEnd != len(text) {
		t.Errorf("last chunk ByteEnd = %d, want %d", last.ByteEnd, len(text))
	}
}
