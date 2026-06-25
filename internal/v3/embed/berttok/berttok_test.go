package berttok

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// goldenFile mirrors testdata/bert_golden.json, generated from the REAL
// HuggingFace tokenizer (tokenizers lib + the model's tokenizer.json). The
// pure-Go tokenizer must reproduce input_ids/attention_mask/type_ids exactly.
type goldenFile struct {
	MaxLen int `json:"max_len"`
	Cases  map[string]struct {
		Text   string   `json:"text"`
		IDs    []int64  `json:"input_ids"`
		Mask   []int64  `json:"attention_mask"`
		Types  []int64  `json:"type_ids"`
		Tokens []string `json:"tokens"`
	} `json:"cases"`
}

func loadGolden(t *testing.T) goldenFile {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "bert_golden.json"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var g goldenFile
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatalf("parse golden: %v", err)
	}
	return g
}

func TestEncodeMatchesHFGolden(t *testing.T) {
	g := loadGolden(t)
	tok, err := New(filepath.Join("testdata", "tokenizer.json"), g.MaxLen)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for name, c := range g.Cases {
		c := c
		t.Run(name, func(t *testing.T) {
			ids, mask, types := tok.Encode(c.Text)
			assertEqInt64(t, "input_ids", ids, c.IDs, c.Tokens)
			assertEqInt64(t, "attention_mask", mask, c.Mask, c.Tokens)
			assertEqInt64(t, "type_ids", types, c.Types, c.Tokens)
		})
	}
}

func assertEqInt64(t *testing.T, field string, got, want []int64, wantTokens []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: length got %d, want %d", field, len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s[%d]: got %d, want %d\n  want tokens: %v", field, i, got[i], want[i], firstN(wantTokens, 24))
		}
	}
}

func firstN(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// TestIsChineseCharBoundary locks the CJK-Extension-C block boundary. The HF
// Rust `tokenizers` lib (the parity oracle) starts that block at U+2B920, NOT
// U+2B820 as in Google/HF-Python BERT. Values verified directly against the
// model's tokenizer.json normalizer.
func TestIsChineseCharBoundary(t *testing.T) {
	cases := map[rune]bool{
		0x4E00:  true,  // CJK Unified start
		0x2B81F: true,  // end of the prior block
		0x2B820: false, // <- the divergence: Rust does NOT treat this as CJK
		0x2B850: false,
		0x2B91F: false,
		0x2B920: true, // <- Rust block actually starts here
		0x2B921: true,
		0x2CEAF: true,  // block end
		0x0041:  false, // 'A'
		0xF8FF:  false, // private use, just before the F900 block
		0xF900:  true,  // CJK compat block start
	}
	for r, want := range cases {
		if got := isChineseChar(r); got != want {
			t.Errorf("isChineseChar(U+%04X) = %v, want %v", r, got, want)
		}
	}
}
