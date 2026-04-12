//go:build cgo

package chunker

import (
	"strings"
)

// ProseChunk represents a text chunk with byte offsets relative to the
// original input and a token count.
type ProseChunk struct {
	Text       string
	ByteStart  int
	ByteEnd    int
	TokenCount int
	Header     string // nearest Markdown heading, if any
}

// Counter abstracts the token-count implementation so tests can pass a stub.
type Counter interface {
	Count(s string) int
}

// Config controls the recursive prose chunker's target sizes.
type Config struct {
	TargetTokens int // default 200
	MinTokens    int // default 80
	MaxTokens    int // default 260
	OverlapToks  int // default 30 (deferred to M2 per plan)
}

// DefaultConfig returns the spec-recommended chunk sizes.
func DefaultConfig() Config {
	return Config{TargetTokens: 200, MinTokens: 80, MaxTokens: 260, OverlapToks: 30}
}

// ChunkProse recursively splits text along paragraph/line/sentence/word
// boundaries until each piece fits within MaxTokens.
// Returns byte offsets relative to the original input.
func ChunkProse(text string, cfg Config, counter Counter) []ProseChunk {
	separators := []string{"\n\n", "\n", ". ", " "}
	return recursiveSplit(text, 0, separators, cfg, counter)
}

func recursiveSplit(text string, offset int, seps []string, cfg Config, c Counter) []ProseChunk {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	toks := c.Count(text)
	if toks <= cfg.MaxTokens {
		return []ProseChunk{{
			Text:       text,
			ByteStart:  offset,
			ByteEnd:    offset + len(text),
			TokenCount: toks,
		}}
	}
	if len(seps) == 0 {
		// Hard-cut by byte length proportional to token count.
		cut := len(text) * cfg.TargetTokens / toks
		if cut < 1 {
			cut = 1
		}
		left := text[:cut]
		right := text[cut:]
		out := []ProseChunk{{
			Text: left, ByteStart: offset, ByteEnd: offset + cut, TokenCount: c.Count(left),
		}}
		out = append(out, recursiveSplit(right, offset+cut, nil, cfg, c)...)
		return out
	}
	sep := seps[0]
	parts := strings.Split(text, sep)

	var out []ProseChunk
	var buf strings.Builder
	bufStart := offset
	for i, p := range parts {
		candidate := buf.String()
		if candidate != "" {
			candidate += sep
		}
		candidate += p
		if c.Count(candidate) > cfg.TargetTokens && buf.Len() > 0 {
			// Emit current buffer
			s := buf.String()
			out = append(out, recursiveSplit(s, bufStart, seps[1:], cfg, c)...)
			buf.Reset()
			bufStart = offset + positionOf(parts, i, sep)
			buf.WriteString(p)
			continue
		}
		if buf.Len() > 0 {
			buf.WriteString(sep)
		}
		buf.WriteString(p)
	}
	if buf.Len() > 0 {
		out = append(out, recursiveSplit(buf.String(), bufStart, seps[1:], cfg, c)...)
	}
	return out
}

func positionOf(parts []string, i int, sep string) int {
	n := 0
	for j := 0; j < i; j++ {
		n += len(parts[j]) + len(sep)
	}
	return n
}
