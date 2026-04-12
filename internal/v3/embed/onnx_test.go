//go:build cgo

package embed

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEmbedderInitIfModelPresent(t *testing.T) {
	modelDir := "./model"
	modelPath := filepath.Join(modelDir, "model.onnx")
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		t.Skip("model.onnx not present — run `make v3-fetch-model`")
	}
	e := NewEmbedder(modelPath, filepath.Join(modelDir, "tokenizer.json"))
	if err := e.init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	defer func() { _ = e.Close() }()
}
