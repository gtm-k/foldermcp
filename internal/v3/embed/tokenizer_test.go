//go:build cgo

package embed

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEmbedderCachesTokenizer (D28b-E2 follow-up A): the tokenizer is
// parsed once per Embedder, not once per call — pointer identity across
// two loads proves the second call did not re-read tokenizer.json. Uses
// a minimal fake vocab so the test runs without the real model files.
func TestEmbedderCachesTokenizer(t *testing.T) {
	dir := t.TempDir()
	tp := filepath.Join(dir, "tokenizer.json")
	fake := `{"model":{"vocab":{"[CLS]":101,"[SEP]":102,"[UNK]":100,"[PAD]":0,"hello":7592}}}`
	if err := os.WriteFile(tp, []byte(fake), 0o644); err != nil {
		t.Fatal(err)
	}

	e := NewEmbedder(filepath.Join(dir, "missing-model.onnx"), tp)
	t1, err := e.loadTokenizer()
	if err != nil {
		t.Fatalf("first loadTokenizer: %v", err)
	}
	t2, err := e.loadTokenizer()
	if err != nil {
		t.Fatalf("second loadTokenizer: %v", err)
	}
	if t1 != t2 {
		t.Error("second loadTokenizer returned a different instance — tokenizer was reloaded")
	}
}

// TestEmbedderTokenizerErrorIsSticky: a bad tokenizer path fails on
// every load attempt without panicking, and Init reports it (the Runner
// treats that as fatal — pre-mortem Story 1).
func TestEmbedderTokenizerErrorIsSticky(t *testing.T) {
	e := NewEmbedder("missing-model.onnx", filepath.Join(t.TempDir(), "absent-tokenizer.json"))
	if _, err := e.loadTokenizer(); err == nil {
		t.Fatal("loadTokenizer with absent file: want error")
	}
	if _, err := e.loadTokenizer(); err == nil {
		t.Fatal("second loadTokenizer: want sticky error")
	}
	if err := e.Init(); err == nil {
		t.Fatal("Init with absent tokenizer: want error (fail-fast contract)")
	}
}

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
