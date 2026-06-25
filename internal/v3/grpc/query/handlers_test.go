//go:build cgo

package query

import (
	"context"
	"testing"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestFilenameSearchFindsMatch(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, `INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
		VALUES('/docs/notes/meeting.md',X'CC',100,1000,'text/markdown','document','/docs/notes/',1)`)
	mustExec(t, db, `INSERT INTO nodes(file_id,node_type,name,provenance,created_at,updated_at)
		VALUES(1,'doc','meeting.md','EXTRACTED',1,1)`)

	h := &FilenameHandler{DB: db}
	resp, err := h.Search(context.Background(), &pb.FilenameSearchRequest{Query: "meeting", K: 10})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status.Status != "OK" {
		t.Errorf("status = %s, want OK", resp.Status.Status)
	}
	if len(resp.Results) != 1 {
		t.Errorf("results = %d, want 1", len(resp.Results))
	}
}

func TestFilenameSearchEmptyQuery(t *testing.T) {
	db := openTestDB(t)
	h := &FilenameHandler{DB: db}
	resp, err := h.Search(context.Background(), &pb.FilenameSearchRequest{Query: "", K: 10})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status.Status != "EMPTY_BUT_EXECUTED" {
		t.Errorf("status = %s, want EMPTY_BUT_EXECUTED", resp.Status.Status)
	}
}

func TestMetadataSearchByContentClass(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, `INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
		VALUES('/a.py',X'DD',50,2000,'text/x-python','code','/','1')`)
	mustExec(t, db, `INSERT INTO nodes(file_id,node_type,name,provenance,created_at,updated_at)
		VALUES(1,'module','a.py','EXTRACTED',1,1)`)
	mustExec(t, db, `INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
		VALUES('/b.md',X'EE',30,1000,'text/markdown','document','/','1')`)
	mustExec(t, db, `INSERT INTO nodes(file_id,node_type,name,provenance,created_at,updated_at)
		VALUES(2,'doc','b.md','EXTRACTED',1,1)`)

	h := &MetadataHandler{DB: db}
	resp, err := h.Search(context.Background(), &pb.MetadataSearchRequest{
		ContentClass: "code",
		K:            10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status.Status != "OK" {
		t.Errorf("status = %s, want OK", resp.Status.Status)
	}
	if len(resp.Results) != 1 {
		t.Errorf("results = %d, want 1 (only code)", len(resp.Results))
	}
}

func TestGetNodesReturnsSeededNode(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, `INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
		VALUES('/x.go',X'FF',10,1,'text/x-go','code','/',1)`)
	mustExec(t, db, `INSERT INTO nodes(file_id,node_type,name,provenance,created_at,updated_at)
		VALUES(1,'function','main','EXTRACTED',1,1)`)

	h := &NodesHandler{DB: db}
	resp, err := h.Get(context.Background(), &pb.GetNodesRequest{NodeIds: []int64{1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(resp.Nodes))
	}
	if resp.Nodes[0].Name != "main" {
		t.Errorf("name = %s, want main", resp.Nodes[0].Name)
	}
}

func TestGetChunksReturnsSeededChunk(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, `INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
		VALUES('/x.md',X'AA',1,1,'text/markdown','document','/',1)`)
	mustExec(t, db, `INSERT INTO nodes(file_id,node_type,name,provenance,created_at,updated_at)
		VALUES(1,'doc','x','EXTRACTED',1,1)`)
	mustExec(t, db, `INSERT INTO chunks(node_id,text,byte_start,byte_end,token_count,chunk_kind)
		VALUES(1,'test chunk text',0,15,3,'prose')`)

	h := &ChunksHandler{DB: db}
	resp, err := h.Get(context.Background(), &pb.GetChunksRequest{ChunkIds: []int64{1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Chunks) != 1 {
		t.Fatalf("chunks = %d, want 1", len(resp.Chunks))
	}
	if resp.Chunks[0].Text != "test chunk text" {
		t.Errorf("text = %s", resp.Chunks[0].Text)
	}
}

func TestGraphExpandReturnsStub(t *testing.T) {
	db := openTestDB(t)
	h := &GraphHandler{DB: db}
	resp, err := h.Expand(context.Background(), &pb.GraphExpandRequest{SeedNodeIds: []int64{1}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status.Status != "EMPTY_BUT_EXECUTED" {
		t.Errorf("status = %s, want EMPTY_BUT_EXECUTED", resp.Status.Status)
	}
}

func TestGetBlobNotFoundInM1(t *testing.T) {
	db := openTestDB(t)
	h := &BlobsHandler{DB: db}
	_, err := h.Get(context.Background(), &pb.GetBlobRequest{BlobId: 999})
	if err == nil {
		t.Fatal("expected NOT_FOUND error for missing blob")
	}
	// Verify it's a gRPC NOT_FOUND status
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %T: %v", err, err)
	}
	if st.Code() != codes.NotFound {
		t.Errorf("code = %v, want NotFound", st.Code())
	}
}
