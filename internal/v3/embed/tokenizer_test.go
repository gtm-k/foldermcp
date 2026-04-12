//go:build cgo

package embed

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTokenizerEncodeSkipsIfMissing(t *testing.T) {
	tp := filepath.Join(".", "model", "tokenizer.json")
	if _, err := os.Stat(tp); os.IsNotExist(err) {
		t.Skip("tokenizer.json not present — run `make v3-fetch-model`")
	}
	tok, err := LoadTokenizer(tp)
	if err != nil {
		t.Fatal(err)
	}
	ids, mask, tts := tok.Encode("hello world")
	if len(ids) != MaxSeqLen {
		t.Errorf("ids len = %d, want %d", len(ids), MaxSeqLen)
	}
	if len(mask) != MaxSeqLen || len(tts) != MaxSeqLen {
		t.Error("mask/tts length mismatch")
	}
	if ids[0] != tok.clsID {
		t.Errorf("ids[0] = %d, want CLS (%d)", ids[0], tok.clsID)
	}
	// "hello world" → [CLS] hello world [SEP] [PAD]...
	// At least 4 tokens should have mask=1
	var maskSum int64
	for _, m := range mask {
		maskSum += m
	}
	if maskSum < 4 {
		t.Errorf("mask sum = %d, expected ≥4", maskSum)
	}
}
