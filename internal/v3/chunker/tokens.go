//go:build cgo

package chunker

import (
	"strings"
	"sync"

	"github.com/pkoukk/tiktoken-go"
)

// TiktokenCounter implements Counter using the cl100k_base encoding
// (Claude/GPT-4 family). It is goroutine-safe via sync.Once initialization.
type TiktokenCounter struct {
	once sync.Once
	enc  *tiktoken.Tiktoken
	err  error
}

func NewTiktokenCounter() *TiktokenCounter { return &TiktokenCounter{} }

func (t *TiktokenCounter) init() {
	t.once.Do(func() {
		enc, err := tiktoken.GetEncoding("cl100k_base")
		if err != nil {
			t.err = err
			return
		}
		t.enc = enc
	})
}

// Count returns the token count for s using cl100k_base.
// Falls back to word-count approximation if the encoding fails to load.
func (t *TiktokenCounter) Count(s string) int {
	t.init()
	if t.err != nil || t.enc == nil {
		// Degrade gracefully: approximate as 1 token per 4 runes
		return len(strings.Fields(s))
	}
	return len(t.enc.Encode(s, nil, nil))
}
