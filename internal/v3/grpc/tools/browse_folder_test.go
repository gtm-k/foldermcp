//go:build cgo

package tools

import (
	"context"
	"database/sql"
	"encoding/base64"
	"testing"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
)

// mustSeed inserts a file with parent_dir = filepath.Dir(path)-style value
// (no trailing slash) — matching what the real walker stores.
func mustSeed(t *testing.T, db *sql.DB, path, parent, class string) {
	t.Helper()
	mustExec(t, db, `INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
		VALUES(?,X'AA',10,100,'text/plain',?,?,1)`, path, class, parent)
}

func TestBrowseListsFilesAndSubdirs(t *testing.T) {
	db := openTestDB(t)
	mustSeed(t, db, "/root/a.go", "/root", "code")
	mustSeed(t, db, "/root/b.md", "/root", "document")
	mustSeed(t, db, "/root/sub/c.go", "/root/sub", "code")
	mustSeed(t, db, "/root/sub/deep/d.go", "/root/sub/deep", "code")
	mustSeed(t, db, "/other/e.txt", "/other", "document")

	h := &BrowseHandler{DB: db}
	resp, err := h.Browse(context.Background(), &pb.BrowseFolderRequest{Path: "/root"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status.Status != "OK" {
		t.Fatalf("status = %s, want OK", resp.Status.Status)
	}
	// Expect a.go, b.md (files) + sub (dir). NOT c.go/d.go (deeper).
	var files, dirs []string
	for _, e := range resp.Entries {
		if e.IsDir {
			dirs = append(dirs, e.Name)
		} else {
			files = append(files, e.Name)
		}
	}
	if len(files) != 2 {
		t.Errorf("files = %v, want [a.go b.md]", files)
	}
	if len(dirs) != 1 || dirs[0] != "sub" {
		t.Errorf("dirs = %v, want [sub] with IsDir=true", dirs)
	}
}

func TestBrowsePathNormalizationTrailingSlash(t *testing.T) {
	db := openTestDB(t)
	mustSeed(t, db, "/root/a.go", "/root", "code")
	mustSeed(t, db, "/root/sub/c.go", "/root/sub", "code")

	h := &BrowseHandler{DB: db}
	// Trailing slash must normalize to the same result as no trailing slash.
	withSlash, err := h.Browse(context.Background(), &pb.BrowseFolderRequest{Path: "/root/"})
	if err != nil {
		t.Fatal(err)
	}
	noSlash, err := h.Browse(context.Background(), &pb.BrowseFolderRequest{Path: "/root"})
	if err != nil {
		t.Fatal(err)
	}
	if len(withSlash.Entries) != len(noSlash.Entries) || len(noSlash.Entries) == 0 {
		t.Errorf("trailing-slash mismatch: %d vs %d (want equal, non-zero)", len(withSlash.Entries), len(noSlash.Entries))
	}
}

func TestBrowseNormalizesRedundantPath(t *testing.T) {
	db := openTestDB(t)
	mustSeed(t, db, "/root/a.go", "/root", "code")
	h := &BrowseHandler{DB: db}
	// Redundant trailing slashes and "." segments must clean to one listing.
	for _, p := range []string{"/root", "/root/", "/root/.", "/root//"} {
		resp, err := h.Browse(context.Background(), &pb.BrowseFolderRequest{Path: p})
		if err != nil {
			t.Fatal(err)
		}
		if resp.Status.Status != "OK" || len(resp.Entries) != 1 {
			t.Errorf("path %q -> status=%s entries=%d, want OK with 1 entry", p, resp.Status.Status, len(resp.Entries))
		}
	}
}

func TestBrowseSubdirListing(t *testing.T) {
	db := openTestDB(t)
	mustSeed(t, db, "/root/sub/c.go", "/root/sub", "code")
	mustSeed(t, db, "/root/sub/deep/d.go", "/root/sub/deep", "code")

	h := &BrowseHandler{DB: db}
	resp, err := h.Browse(context.Background(), &pb.BrowseFolderRequest{Path: "/root/sub"})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	dir := false
	for _, e := range resp.Entries {
		names = append(names, e.Name)
		if e.Name == "deep" && e.IsDir {
			dir = true
		}
	}
	if !dir {
		t.Errorf("expected subdir 'deep' (IsDir), got entries %v", names)
	}
}

func TestBrowseNotFoundIsDistinct(t *testing.T) {
	db := openTestDB(t)
	mustSeed(t, db, "/root/a.go", "/root", "code")

	h := &BrowseHandler{DB: db}
	resp, err := h.Browse(context.Background(), &pb.BrowseFolderRequest{Path: "/does/not/exist"})
	if err != nil {
		t.Fatal(err)
	}
	// Actor-observability: a wrong path must NOT look like an empty-but-valid dir.
	if resp.Status.Status != "NOT_FOUND" {
		t.Errorf("status = %s, want NOT_FOUND for a path with no indexed entries", resp.Status.Status)
	}
	if len(resp.Entries) != 0 {
		t.Errorf("entries = %d, want 0", len(resp.Entries))
	}
}

func TestBrowseRootListsTopDirs(t *testing.T) {
	db := openTestDB(t)
	mustSeed(t, db, "/root/a.go", "/root", "code")
	mustSeed(t, db, "/other/e.txt", "/other", "document")

	h := &BrowseHandler{DB: db}
	resp, err := h.Browse(context.Background(), &pb.BrowseFolderRequest{Path: "/"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status.Status != "OK" {
		t.Fatalf("status = %s, want OK", resp.Status.Status)
	}
	dirs := map[string]bool{}
	for _, e := range resp.Entries {
		if e.IsDir {
			dirs[e.Name] = true
		}
	}
	if !dirs["root"] || !dirs["other"] {
		t.Errorf("root listing dirs = %v, want root+other", dirs)
	}
}

func TestBrowsePagination(t *testing.T) {
	db := openTestDB(t)
	mustSeed(t, db, "/p/a.txt", "/p", "document")
	mustSeed(t, db, "/p/b.txt", "/p", "document")
	mustSeed(t, db, "/p/c.txt", "/p", "document")

	h := &BrowseHandler{DB: db}
	resp, err := h.Browse(context.Background(), &pb.BrowseFolderRequest{Path: "/p", MaxItems: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Entries) != 2 {
		t.Fatalf("page1 entries = %d, want 2", len(resp.Entries))
	}
	if resp.NextCursor == "" {
		t.Fatal("expected next_cursor for pagination")
	}
	resp2, err := h.Browse(context.Background(), &pb.BrowseFolderRequest{Path: "/p", MaxItems: 2, Cursor: resp.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp2.Entries) != 1 {
		t.Fatalf("page2 entries = %d, want 1", len(resp2.Entries))
	}
	if resp2.NextCursor != "" {
		t.Errorf("page2 should have no next_cursor, got %q", resp2.NextCursor)
	}
}

func TestBrowseCursorDecodeFailGraceful(t *testing.T) {
	db := openTestDB(t)
	mustSeed(t, db, "/q/a.txt", "/q", "document")
	h := &BrowseHandler{DB: db}
	resp, err := h.Browse(context.Background(), &pb.BrowseFolderRequest{Path: "/q", Cursor: "not-valid-base64!!!"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status.Status != "OK" {
		t.Errorf("status = %s, want OK", resp.Status.Status)
	}
	badCursor := base64.StdEncoding.EncodeToString([]byte("abc"))
	resp2, err := h.Browse(context.Background(), &pb.BrowseFolderRequest{Path: "/q", Cursor: badCursor})
	if err != nil {
		t.Fatal(err)
	}
	if resp2.Status.Status != "OK" {
		t.Errorf("status = %s, want OK", resp2.Status.Status)
	}
}
