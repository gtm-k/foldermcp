//go:build cgo

package tools

import (
	"context"
	"testing"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
	"github.com/gtm-k/foldermcp/internal/v3/grpc/query"
)

func TestInspectReturnsNode(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, `INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
		VALUES('/src/main.go',X'AA',100,1000,'text/x-go','code','/src/',1)`)
	mustExec(t, db, `INSERT INTO nodes(file_id,node_type,name,provenance,created_at,updated_at)
		VALUES(1,'function','main','EXTRACTED',1,1)`)
	mustExec(t, db, `INSERT INTO chunks(node_id,text,byte_start,byte_end,token_count,chunk_kind)
		VALUES(1,'func main() { fmt.Println("hello") }',0,37,8,'code_ast')`)

	h := &InspectHandler{DB: db, Nodes: &query.NodesHandler{DB: db}}
	resp, err := h.Inspect(context.Background(), &pb.InspectNodeRequest{NodeId: 1, Detail: "standard"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status.Status != "OK" {
		t.Errorf("status = %s, want OK", resp.Status.Status)
	}
	if resp.Node == nil {
		t.Fatal("node is nil")
	}
	if resp.Node.Name != "main" {
		t.Errorf("name = %s, want main", resp.Node.Name)
	}
	if len(resp.Node.Chunks) != 1 {
		t.Errorf("chunks = %d, want 1", len(resp.Node.Chunks))
	}
}

func TestInspectNodeNotFound(t *testing.T) {
	db := openTestDB(t)
	h := &InspectHandler{DB: db, Nodes: &query.NodesHandler{DB: db}}
	resp, err := h.Inspect(context.Background(), &pb.InspectNodeRequest{NodeId: 999})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status.Status != "EMPTY_BUT_EXECUTED" {
		t.Errorf("status = %s, want EMPTY_BUT_EXECUTED", resp.Status.Status)
	}
	if resp.Node != nil {
		t.Error("node should be nil for missing node")
	}
}

func TestInspectDefaultDetailIsBrief(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, `INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
		VALUES('/x.md',X'BB',10,1,'text/markdown','document','/',1)`)
	mustExec(t, db, `INSERT INTO nodes(file_id,node_type,name,provenance,created_at,updated_at)
		VALUES(1,'doc','x','EXTRACTED',1,1)`)
	// Insert 5 chunks
	for i := 0; i < 5; i++ {
		mustExec(t, db, `INSERT INTO chunks(node_id,text,byte_start,byte_end,token_count,chunk_kind)
			VALUES(1,'chunk text',0,10,2,'prose')`)
	}

	h := &InspectHandler{DB: db, Nodes: &query.NodesHandler{DB: db}}
	resp, err := h.Inspect(context.Background(), &pb.InspectNodeRequest{NodeId: 1})
	if err != nil {
		t.Fatal(err)
	}
	// Default detail is "brief" → 1 chunk
	if len(resp.Node.Chunks) != 1 {
		t.Errorf("default detail chunks = %d, want 1 (brief)", len(resp.Node.Chunks))
	}
}
