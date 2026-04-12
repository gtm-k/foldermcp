//go:build cgo

package query

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
	"github.com/gtm-k/foldermcp/internal/v3/store"
)

// openTestDB opens a temp store with migrations applied.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	tmp := t.TempDir()
	db, err := store.Open(store.Options{Path: filepath.Join(tmp, "test.db"), Tier: store.TierMid})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(db, ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query[:min(len(query), 60)], err)
	}
}

func TestFTSSearchFindsSeededChunks(t *testing.T) {
	db := openTestDB(t)

	// Seed one file, one node, two chunks
	mustExec(t, db, `INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
		VALUES('/x.md',X'AA',1,1,'text/markdown','document','/',1)`)
	mustExec(t, db, `INSERT INTO nodes(file_id,node_type,name,provenance,created_at,updated_at)
		VALUES(1,'doc','x','EXTRACTED',1,1)`)
	mustExec(t, db, `INSERT INTO chunks(node_id,text,byte_start,byte_end,token_count,chunk_kind)
		VALUES(1,'the quick brown fox jumps over the lazy dog',0,44,9,'prose')`)
	mustExec(t, db, `INSERT INTO chunks(node_id,text,byte_start,byte_end,token_count,chunk_kind)
		VALUES(1,'hello world greeting message',0,28,5,'prose')`)

	h := &FTSHandler{DB: db}
	resp, err := h.Search(context.Background(), &pb.FTSSearchRequest{Query: "brown fox", K: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if resp.Status.Status != "OK" {
		t.Errorf("status = %s, want OK (err: %s)", resp.Status.Status, resp.Status.ErrorMessage)
	}
	if len(resp.Results) == 0 {
		t.Fatal("expected FTS hits for 'brown fox'")
	}
}

func TestFTSSearchEmptyResult(t *testing.T) {
	db := openTestDB(t)
	h := &FTSHandler{DB: db}
	resp, err := h.Search(context.Background(), &pb.FTSSearchRequest{Query: "nonexistent", K: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if resp.Status.Status != "EMPTY_BUT_EXECUTED" {
		t.Errorf("status = %s, want EMPTY_BUT_EXECUTED", resp.Status.Status)
	}
}
