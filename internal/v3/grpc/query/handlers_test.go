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

// TestSoftDeletedFileExcludedFromHydrate (integration FIX 2): A6 watch reconcile
// SOFT-deletes a dropped file (sets files.deleted_at) and HARD-purges only at the
// next reconcile. Between the two, the file's NODES still have deleted_at NULL, so
// hydrateNodes (which filtered only n.deleted_at) and GetNodes still returned its
// content — leaking a deleted file for up to one reconcile interval. The fix adds
// `AND f.deleted_at IS NULL` at the hydrate/GetNodes choke points so exclusion is
// IMMEDIATE the instant reconcile flags the file. This drives a vector + FTS +
// hydrate round-trip plus a direct GetNodes against a soft-deleted file whose node
// has deleted_at NULL and asserts its content is absent from every path.
func TestSoftDeletedFileExcludedFromHydrate(t *testing.T) {
	db := openTestDB(t)

	// file 1: LIVE. file 2: SOFT-DELETED (files.deleted_at set) but its NODE row
	// has deleted_at NULL — exactly the reconcile interim window.
	mustExec(t, db, `INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
		VALUES('/live.md',X'AA',1,1,'text/markdown','document','/',1)`)
	mustExec(t, db, `INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen,deleted_at)
		VALUES('/gone.md',X'BB',1,1,'text/markdown','document','/',1,12345)`)
	mustExec(t, db, `INSERT INTO nodes(node_id,file_id,node_type,name,provenance,created_at,updated_at)
		VALUES(1,1,'doc','live','EXTRACTED',1,1)`)
	mustExec(t, db, `INSERT INTO nodes(node_id,file_id,node_type,name,provenance,created_at,updated_at)
		VALUES(2,2,'doc','gone','EXTRACTED',1,1)`) // deleted_at NULL on the node
	mustExec(t, db, `INSERT INTO chunks(chunk_id,node_id,text,byte_start,byte_end,token_count,chunk_kind)
		VALUES(1,1,'shared probe phrase tungsten',0,28,4,'prose')`)
	mustExec(t, db, `INSERT INTO chunks(chunk_id,node_id,text,byte_start,byte_end,token_count,chunk_kind)
		VALUES(2,2,'shared probe phrase tungsten',0,28,4,'prose')`)
	vec := make([]byte, 384)
	for i := range vec {
		vec[i] = byte(i % 127)
	}
	mustExec(t, db, `INSERT INTO embeddings(chunk_id, embedding) VALUES(1, vec_int8(?))`, vec)
	mustExec(t, db, `INSERT INTO embeddings(chunk_id, embedding) VALUES(2, vec_int8(?))`, vec)

	hint := &pb.HydrationHint{Chunks: true, ChunksPerNode: 5}

	// Vector path: both nodes may rank, but the soft-deleted node must NOT hydrate.
	vh := &VectorHandler{DB: db}
	vresp, err := vh.Search(context.Background(), &pb.VectorSearchRequest{QueryEmbeddingInt8: vec, K: 10, Hydrate: hint})
	if err != nil {
		t.Fatalf("vector search: %v", err)
	}
	assertSoftDeletedHydrationExcluded(t, "vector", vresp.Results)

	// FTS path: same invariant.
	fh := &FTSHandler{DB: db}
	fresp, err := fh.Search(context.Background(), &pb.FTSSearchRequest{Query: "tungsten", K: 10, Hydrate: hint})
	if err != nil {
		t.Fatalf("fts search: %v", err)
	}
	assertSoftDeletedHydrationExcluded(t, "fts", fresp.Results)

	// Direct GetNodes by id must also exclude the soft-deleted file's node.
	nh := &NodesHandler{DB: db}
	nresp, err := nh.Get(context.Background(), &pb.GetNodesRequest{NodeIds: []int64{1, 2}, Hydrate: hint})
	if err != nil {
		t.Fatalf("get_nodes: %v", err)
	}
	for _, n := range nresp.Nodes {
		if n.NodeId == 2 {
			t.Errorf("GetNodes returned node 2 whose file is soft-deleted (deleted_at set) — must be excluded immediately")
		}
	}
	if len(nresp.Nodes) != 1 || nresp.Nodes[0].NodeId != 1 {
		t.Errorf("GetNodes nodes = %v, want exactly [node 1]", nodeIDs(nresp.Nodes))
	}
}

func assertSoftDeletedHydrationExcluded(t *testing.T, path string, results []*pb.ScoredNode) {
	t.Helper()
	sawLive := false
	for _, s := range results {
		switch s.NodeId {
		case 1:
			sawLive = true
			if s.Hydrated == nil {
				t.Errorf("%s: live node 1 was not hydrated", path)
			}
		case 2:
			if s.Hydrated != nil {
				t.Errorf("%s: soft-deleted file's node 2 was hydrated (path=%q, chunks=%d) — content leaked despite files.deleted_at",
					path, s.Hydrated.Path, len(s.Hydrated.Chunks))
			}
		}
	}
	if !sawLive {
		t.Errorf("%s: live node 1 absent from results — fixture/search problem", path)
	}
}

func nodeIDs(nodes []*pb.HydratedNode) []int64 {
	out := make([]int64, len(nodes))
	for i, n := range nodes {
		out[i] = n.NodeId
	}
	return out
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
