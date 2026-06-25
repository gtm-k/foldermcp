//go:build cgo

package embed

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sync"
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
	// D28b-E2 follow-up A: the first EmbedQuery call must have cached
	// the parsed tokenizer on the Embedder.
	tokAfterFirst := e.tokenizer
	if tokAfterFirst == nil {
		t.Fatal("tokenizer not cached after first EmbedQuery")
	}
	// Same query twice should be identical (deterministic)
	vec2, err := e.EmbedQuery("session handling")
	if err != nil {
		t.Fatal(err)
	}
	if e.tokenizer != tokAfterFirst {
		t.Error("second EmbedQuery reloaded the tokenizer — cache broken")
	}
	for i := range vec1 {
		if vec1[i] != vec2[i] {
			t.Errorf("non-deterministic at %d", i)
			break
		}
	}
}

// TestEmbedQueryConcurrentIfModelPresent (E2 review finding 1): one
// query-side Embedder is shared across concurrent gRPC search handlers,
// and Embed() does unsynchronized copy() into shared pre-allocated ORT
// tensors plus session.Run(). EmbedQuery must serialize internally —
// without the mutex this test fails under -race and/or returns
// corrupted vectors.
func TestEmbedQueryConcurrentIfModelPresent(t *testing.T) {
	md := "./model"
	if _, err := os.Stat(filepath.Join(md, "model.onnx")); os.IsNotExist(err) {
		t.Skip("model.onnx not present")
	}
	e := NewEmbedder(
		filepath.Join(md, "model.onnx"),
		filepath.Join(md, "tokenizer.json"),
	)
	defer func() { _ = e.Close() }()

	// Reference vectors, computed sequentially (EmbedQuery is deterministic).
	queries := []string{"session handling", "config parsing", "vector search"}
	want := make([][]byte, len(queries))
	for i, q := range queries {
		v, err := e.EmbedQuery(q)
		if err != nil {
			t.Fatal(err)
		}
		want[i] = v
	}

	const workers = 8
	const iters = 5
	var wg sync.WaitGroup
	errCh := make(chan error, workers*iters)
	for w := range workers {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := range iters {
				qi := (w + i) % len(queries)
				got, err := e.EmbedQuery(queries[qi])
				if err != nil {
					errCh <- fmt.Errorf("worker %d iter %d: %w", w, i, err)
					return
				}
				if !bytes.Equal(got, want[qi]) {
					errCh <- fmt.Errorf("worker %d iter %d: corrupted vector for %q — concurrent EmbedQuery not serialized", w, i, queries[qi])
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
}
