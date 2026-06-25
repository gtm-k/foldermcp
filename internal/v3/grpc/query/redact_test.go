//go:build cgo

package query

import (
	"context"
	"strings"
	"testing"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
)

// fakeAWSKey is a synthetic (non-functional) AWS access key id used to simulate
// a secret that reached the index BEFORE Phase-7 ingest redaction shipped — its
// chunk text was never ingest-redacted, so egress is the only layer that can
// catch it. NEVER a real key.
const fakeAWSKey = "AKIAIOSFODNN7EXAMPLE"

// TestGetChunks_EgressRedactsPreRedactionChunk (MED-3): a chunk whose RAW text
// still carries a secret (a pre-Phase-7 index, or an ingest miss) must be
// redacted at the GetChunks egress path — the low-level IndexQuery RPC that
// previously returned raw text to any same-user client.
func TestGetChunks_EgressRedactsPreRedactionChunk(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, `INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
		VALUES('/x.md',X'AA',1,1,'text/markdown','document','/',1)`)
	mustExec(t, db, `INSERT INTO nodes(file_id,node_type,name,provenance,created_at,updated_at)
		VALUES(1,'doc','x','EXTRACTED',1,1)`)
	// Insert a RAW secret directly (bypasses ingest redaction — simulates a
	// pre-Phase-7 chunk).
	mustExec(t, db, `INSERT INTO chunks(node_id,text,byte_start,byte_end,token_count,chunk_kind)
		VALUES(1,?,0,40,5,'prose')`, "deploy with key "+fakeAWSKey+" now")

	h := &ChunksHandler{DB: db}
	resp, err := h.Get(context.Background(), &pb.GetChunksRequest{ChunkIds: []int64{1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Chunks) != 1 {
		t.Fatalf("chunks = %d, want 1", len(resp.Chunks))
	}
	if strings.Contains(resp.Chunks[0].Text, fakeAWSKey) {
		t.Errorf("GetChunks returned raw secret: %q — egress redaction missing", resp.Chunks[0].Text)
	}
	if !strings.Contains(resp.Chunks[0].Text, "[REDACTED]") {
		t.Errorf("GetChunks text not redacted: %q", resp.Chunks[0].Text)
	}
}

// TestGetNodes_EgressRedactsPropertiesJson (MED-3): node PropertiesJson can carry
// a secret in a Go symbol signature (e.g. `const apiKey = "..."`). GetNodes must
// redact it at egress.
func TestGetNodes_EgressRedactsPropertiesJson(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, `INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
		VALUES('/x.go',X'FF',10,1,'text/x-go','code','/',1)`)
	props := `{"signature":"const apiKey = \"` + fakeAWSKey + `\""}`
	mustExec(t, db, `INSERT INTO nodes(file_id,node_type,name,properties,provenance,created_at,updated_at)
		VALUES(1,'const','apiKey',?,'EXTRACTED',1,1)`, props)

	h := &NodesHandler{DB: db}
	resp, err := h.Get(context.Background(), &pb.GetNodesRequest{NodeIds: []int64{1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(resp.Nodes))
	}
	if strings.Contains(resp.Nodes[0].PropertiesJson, fakeAWSKey) {
		t.Errorf("GetNodes returned raw secret in PropertiesJson: %q", resp.Nodes[0].PropertiesJson)
	}
	if !strings.Contains(resp.Nodes[0].PropertiesJson, "[REDACTED]") {
		t.Errorf("PropertiesJson not redacted: %q", resp.Nodes[0].PropertiesJson)
	}
}
