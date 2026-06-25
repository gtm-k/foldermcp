//go:build cgo

package query

import (
	"context"
	"testing"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
)

func TestBatchMixedSuccess(t *testing.T) {
	db := openTestDB(t)

	mustExec(t, db, `INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
		VALUES('/a.md',X'AA',1,1,'text/markdown','document','/',1)`)
	mustExec(t, db, `INSERT INTO nodes(file_id,node_type,name,provenance,created_at,updated_at)
		VALUES(1,'doc','a','EXTRACTED',1,1)`)
	mustExec(t, db, `INSERT INTO chunks(node_id,text,byte_start,byte_end,token_count,chunk_kind)
		VALUES(1,'hello world',0,11,2,'prose')`)

	h := &BatchHandler{
		FTS:      &FTSHandler{DB: db},
		Filename: &FilenameHandler{DB: db},
		Metadata: &MetadataHandler{DB: db},
		Graph:    &GraphHandler{DB: db},
		Nodes:    &NodesHandler{DB: db},
		Chunks:   &ChunksHandler{DB: db},
		Vector:   &VectorHandler{DB: db},
	}
	req := &pb.BatchRequest{
		Ops: []*pb.BatchOp{
			{Op: &pb.BatchOp_Fts{Fts: &pb.FTSSearchRequest{Query: "hello", K: 5}}},
			{Op: &pb.BatchOp_Filename{Filename: &pb.FilenameSearchRequest{Query: "a", K: 5}}},
			{Op: &pb.BatchOp_GraphExpand{GraphExpand: &pb.GraphExpandRequest{SeedNodeIds: []int64{1}}}},
		},
	}
	resp, err := h.Batch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Results) != 3 {
		t.Errorf("results = %d, want 3", len(resp.Results))
	}
	// FTS should find "hello"
	if ftsRes := resp.Results[0].GetFts(); ftsRes == nil {
		t.Error("expected FTS result")
	} else if ftsRes.Status.Status != "OK" {
		t.Errorf("fts status = %s", ftsRes.Status.Status)
	}
	// Graph expand should be stub
	if graphRes := resp.Results[2].GetGraphExpand(); graphRes == nil {
		t.Error("expected GraphExpand result")
	} else if graphRes.Status.Status != "EMPTY_BUT_EXECUTED" {
		t.Errorf("graph status = %s", graphRes.Status.Status)
	}
}

func TestBatchRejectsTooManyOps(t *testing.T) {
	db := openTestDB(t)
	h := &BatchHandler{FTS: &FTSHandler{DB: db}}
	ops := make([]*pb.BatchOp, MaxBatchOps+1)
	for i := range ops {
		ops[i] = &pb.BatchOp{Op: &pb.BatchOp_Fts{Fts: &pb.FTSSearchRequest{Query: "x", K: 1}}}
	}
	_, err := h.Batch(context.Background(), &pb.BatchRequest{Ops: ops})
	if err == nil {
		t.Fatal("expected error for too many ops")
	}
}
