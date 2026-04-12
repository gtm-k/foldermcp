//go:build cgo

package query

import (
	"context"
	"testing"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
)

func TestVectorSearchReturnsClosestSeed(t *testing.T) {
	db := openTestDB(t)

	// Seed file → node → chunk → embedding
	mustExec(t, db, `INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
		VALUES('/a',X'BB',1,1,'text/plain','document','/',1)`)
	mustExec(t, db, `INSERT INTO nodes(file_id,node_type,name,provenance,created_at,updated_at)
		VALUES(1,'doc','a','EXTRACTED',1,1)`)
	mustExec(t, db, `INSERT INTO chunks(node_id,text,byte_start,byte_end,token_count,chunk_kind)
		VALUES(1,'seed',0,4,1,'prose')`)

	// Insert one 384-dim int8 vector using vec_int8 wrapper
	vec := make([]byte, 384)
	for i := range vec {
		vec[i] = byte(i % 127)
	}
	mustExec(t, db, `INSERT INTO embeddings(chunk_id, embedding) VALUES(1, vec_int8(?))`, vec)

	h := &VectorHandler{DB: db}
	resp, err := h.Search(context.Background(), &pb.VectorSearchRequest{
		QueryEmbeddingInt8: vec,
		K:                  5,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if resp.Status.Status != "OK" {
		t.Errorf("status = %s, err = %s", resp.Status.Status, resp.Status.ErrorMessage)
	}
	if len(resp.Results) == 0 {
		t.Fatal("expected a vector hit for self-query")
	}
	if resp.Results[0].NodeId != 1 {
		t.Errorf("top hit = %d, want 1", resp.Results[0].NodeId)
	}
}

func TestVectorSearchRejectsBadEmbeddingLen(t *testing.T) {
	db := openTestDB(t)
	h := &VectorHandler{DB: db}
	resp, err := h.Search(context.Background(), &pb.VectorSearchRequest{
		QueryEmbeddingInt8: []byte{1, 2, 3},
		K:                  5,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if resp.Status.Status != "CONTRACT_ERROR" {
		t.Errorf("status = %s, want CONTRACT_ERROR", resp.Status.Status)
	}
}
