//go:build cgo

package query

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gtm-k/foldermcp/internal/v3/embed"
	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
)

// TestVectorSearchRoundTripRealEmbeddings is the regression test the M1 suite
// lacked. It embeds REAL text with the production Embedder, stores it through
// the real write path, and verifies a semantically-related query retrieves the
// right document. This exercises tokenizer -> ONNX -> mean-pool -> quantize ->
// store -> sqlite-vec KNN end to end.
//
// The existing vector tests use hand-crafted vectors and self-queries, which
// structurally cannot catch an embedding/tokenizer defect — exactly the
// placeholder-tokenizer bug that silently shipped (semantic recall 0.12).
//
// Skips if the model/ONNX runtime are unavailable (model is fetched at build
// time; set FOLDERMCP_ORT_LIB so onnxruntime_go can load the shared library).
func TestVectorSearchRoundTripRealEmbeddings(t *testing.T) {
	modelDir := filepath.Join("..", "..", "embed", "model")
	modelPath := filepath.Join(modelDir, "model.onnx")
	tokPath := filepath.Join(modelDir, "tokenizer.json")
	for _, p := range []string{modelPath, tokPath} {
		if _, err := os.Stat(p); os.IsNotExist(err) {
			t.Skipf("%s not present — run `make v3-fetch-model`", p)
		}
	}

	emb := embed.NewEmbedder(modelPath, tokPath)
	defer func() { _ = emb.Close() }()
	if err := emb.Init(); err != nil {
		t.Skipf("embedder init failed (ONNX runtime unavailable?): %v", err)
	}

	db := openTestDB(t)
	docs := []struct {
		id   int64
		text string
	}{
		{1, "cobra parses command-line flags and subcommands for a CLI application"},
		{2, "solar and wind renewable energy power generation and storage"},
		{3, "the flask web framework handles http requests and routing in python"},
	}
	for _, d := range docs {
		mustExec(t, db, `INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
			VALUES(?,X'BB',1,1,'text/plain','document','/',1)`, fmt.Sprintf("/doc%d", d.id))
		mustExec(t, db, `INSERT INTO nodes(node_id,file_id,node_type,name,provenance,created_at,updated_at)
			VALUES(?,?,'doc',?,'EXTRACTED',1,1)`, d.id, d.id, d.text)
		mustExec(t, db, `INSERT INTO chunks(chunk_id,node_id,text,byte_start,byte_end,token_count,chunk_kind)
			VALUES(?,?,?,0,?,1,'prose')`, d.id, d.id, d.text, len(d.text))
		blob, err := emb.EmbedQuery(d.text)
		if err != nil {
			t.Fatalf("embed doc %d: %v", d.id, err)
		}
		mustExec(t, db, `INSERT INTO embeddings(chunk_id, embedding) VALUES(?, vec_int8(?))`, d.id, blob)
	}

	// A query semantically closest to doc 1 (CLI flag parsing).
	q, err := emb.EmbedQuery("how do I parse command line flags in my CLI")
	if err != nil {
		t.Fatalf("embed query: %v", err)
	}

	h := &VectorHandler{DB: db}
	resp, err := h.Search(context.Background(), &pb.VectorSearchRequest{QueryEmbeddingInt8: q, K: 3})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) == 0 {
		t.Fatal("no vector results for a real query")
	}
	if resp.Results[0].NodeId != 1 {
		t.Errorf("top hit node_id = %d, want 1 (the CLI-flags doc) — semantic retrieval is not ranking the relevant document first",
			resp.Results[0].NodeId)
	}
}
