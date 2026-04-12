//go:build cgo

package embed

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEmbedQueryRoundtripIfModelPresent(t *testing.T) {
	md := "./model"
	if _, err := os.Stat(filepath.Join(md, "model.onnx")); os.IsNotExist(err) {
		t.Skip("model.onnx not present")
	}
	e := NewEmbedder(
		filepath.Join(md, "model.onnx"),
		filepath.Join(md, "tokenizer.json"),
	)
	defer func() { _ = e.Close() }()

	vec1, err := e.EmbedQuery("session handling")
	if err != nil {
		t.Fatal(err)
	}
	if len(vec1) != Dimension {
		t.Errorf("len = %d, want %d", len(vec1), Dimension)
	}
	// Same query twice should be identical (deterministic)
	vec2, err := e.EmbedQuery("session handling")
	if err != nil {
		t.Fatal(err)
	}
	for i := range vec1 {
		if vec1[i] != vec2[i] {
			t.Errorf("non-deterministic at %d", i)
			break
		}
	}
}
