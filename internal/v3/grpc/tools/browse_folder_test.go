//go:build cgo

package tools

import (
	"context"
	"encoding/base64"
	"testing"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
)

func TestBrowseFindsChildren(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, `INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
		VALUES('/a/one.go',X'AA',100,1000,'text/x-go','code','/a/',1)`)
	mustExec(t, db, `INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
		VALUES('/a/two.md',X'BB',200,2000,'text/markdown','document','/a/',1)`)
	mustExec(t, db, `INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
		VALUES('/b/three.py',X'CC',50,500,'text/x-python','code','/b/',1)`)

	h := &BrowseHandler{DB: db}
	resp, err := h.Browse(context.Background(), &pb.BrowseFolderRequest{Path: "/a/"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status.Status != "OK" {
		t.Errorf("status = %s, want OK", resp.Status.Status)
	}
	if len(resp.Entries) != 2 {
		t.Fatalf("entries = %d, want 2 (only /a/ children)", len(resp.Entries))
	}
	// Results should be ordered by path
	if resp.Entries[0].Name != "one.go" {
		t.Errorf("first = %s, want one.go", resp.Entries[0].Name)
	}
	if resp.Entries[1].Name != "two.md" {
		t.Errorf("second = %s, want two.md", resp.Entries[1].Name)
	}
}

func TestBrowseEmptyFolder(t *testing.T) {
	db := openTestDB(t)
	h := &BrowseHandler{DB: db}
	resp, err := h.Browse(context.Background(), &pb.BrowseFolderRequest{Path: "/nonexistent/"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status.Status != "OK" {
		t.Errorf("status = %s, want OK", resp.Status.Status)
	}
	if len(resp.Entries) != 0 {
		t.Errorf("entries = %d, want 0", len(resp.Entries))
	}
}

func TestBrowsePagination(t *testing.T) {
	db := openTestDB(t)
	// Insert 3 files in /p/
	mustExec(t, db, `INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
		VALUES('/p/a.txt',X'01',1,1,'text/plain','document','/p/',1)`)
	mustExec(t, db, `INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
		VALUES('/p/b.txt',X'02',1,1,'text/plain','document','/p/',1)`)
	mustExec(t, db, `INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
		VALUES('/p/c.txt',X'03',1,1,'text/plain','document','/p/',1)`)

	h := &BrowseHandler{DB: db}

	// Page 1: max 2 items
	resp, err := h.Browse(context.Background(), &pb.BrowseFolderRequest{Path: "/p/", MaxItems: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Entries) != 2 {
		t.Fatalf("page1 entries = %d, want 2", len(resp.Entries))
	}
	if resp.NextCursor == "" {
		t.Fatal("expected next_cursor for pagination")
	}

	// Page 2: use cursor
	resp2, err := h.Browse(context.Background(), &pb.BrowseFolderRequest{Path: "/p/", MaxItems: 2, Cursor: resp.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp2.Entries) != 1 {
		t.Fatalf("page2 entries = %d, want 1", len(resp2.Entries))
	}
	if resp2.NextCursor != "" {
		t.Errorf("page2 should have no next_cursor, got %q", resp2.NextCursor)
	}
	if resp2.Entries[0].Name != "c.txt" {
		t.Errorf("page2 entry = %s, want c.txt", resp2.Entries[0].Name)
	}
}

func TestBrowseDefaultMaxItems(t *testing.T) {
	db := openTestDB(t)
	h := &BrowseHandler{DB: db}
	// MaxItems=0 should default to 50
	resp, err := h.Browse(context.Background(), &pb.BrowseFolderRequest{Path: "/"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status.Status != "OK" {
		t.Errorf("status = %s, want OK", resp.Status.Status)
	}
}

func TestBrowseCursorDecodeFailGraceful(t *testing.T) {
	db := openTestDB(t)
	h := &BrowseHandler{DB: db}
	// Invalid cursor should be silently ignored (offset stays 0)
	resp, err := h.Browse(context.Background(), &pb.BrowseFolderRequest{
		Path:   "/",
		Cursor: "not-valid-base64!!!",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status.Status != "OK" {
		t.Errorf("status = %s, want OK", resp.Status.Status)
	}
	// Also test valid base64 but non-integer content
	badCursor := base64.StdEncoding.EncodeToString([]byte("abc"))
	resp2, err := h.Browse(context.Background(), &pb.BrowseFolderRequest{
		Path:   "/",
		Cursor: badCursor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp2.Status.Status != "OK" {
		t.Errorf("status = %s, want OK", resp2.Status.Status)
	}
}
