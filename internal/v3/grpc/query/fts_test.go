//go:build cgo

package query

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
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

// seedProseChunk inserts one file+node+chunk so FTS has matchable content.
func seedProseChunk(t *testing.T, db *sql.DB, path, text string) {
	t.Helper()
	var fileID int64
	if err := db.QueryRow(`INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
		VALUES(?,X'AA',1,1,'text/markdown','document','/',1) RETURNING file_id`, path).Scan(&fileID); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	var nodeID int64
	if err := db.QueryRow(`INSERT INTO nodes(file_id,node_type,name,provenance,created_at,updated_at)
		VALUES(?,'doc',?,'EXTRACTED',1,1) RETURNING node_id`, fileID, path).Scan(&nodeID); err != nil {
		t.Fatalf("seed node: %v", err)
	}
	mustExec(t, db, `INSERT INTO chunks(node_id,text,byte_start,byte_end,token_count,chunk_kind)
		VALUES(?,?,0,?,?,'prose')`, nodeID, text, len(text), len(text)/4+1)
}

// TestFTSSearchNaturalLanguageQueryWithPunctuation reproduces the M1/G5 defect:
// a natural-language query containing punctuation ('?') is passed raw into
// FTS5 MATCH, which raises "syntax error near ?" and returns DEGRADED with zero
// results. The handler must instead tokenize the query and return real hits.
func TestFTSSearchNaturalLanguageQueryWithPunctuation(t *testing.T) {
	db := openTestDB(t)
	seedProseChunk(t, db, "/fox.md", "the quick brown fox jumps over the lazy dog")

	h := &FTSHandler{DB: db}
	resp, err := h.Search(context.Background(), &pb.FTSSearchRequest{Query: "where is the brown fox?", K: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if resp.Status.Status != "OK" {
		t.Fatalf("status = %s, want OK (err: %s)", resp.Status.Status, resp.Status.ErrorMessage)
	}
	if len(resp.Results) == 0 {
		t.Fatal("expected FTS hits for natural-language query 'where is the brown fox?'")
	}
}

// TestFTSSearchOrSemanticsAcrossChunks reproduces the implicit-AND half of the
// defect: two query terms that live in different chunks return zero under
// FTS5's default implicit-AND. Lexical retrieval feeding RRF wants OR semantics
// (any-term-match contributes; BM25 ranks), so both chunks must surface.
func TestFTSSearchOrSemanticsAcrossChunks(t *testing.T) {
	db := openTestDB(t)
	seedProseChunk(t, db, "/a.md", "alpha apple pie recipe")
	seedProseChunk(t, db, "/b.md", "beta banana split dessert")

	h := &FTSHandler{DB: db}
	resp, err := h.Search(context.Background(), &pb.FTSSearchRequest{Query: "apple banana", K: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if resp.Status.Status != "OK" {
		t.Fatalf("status = %s, want OK (err: %s)", resp.Status.Status, resp.Status.ErrorMessage)
	}
	if len(resp.Results) < 2 {
		t.Fatalf("expected both chunks via OR semantics, got %d result(s)", len(resp.Results))
	}
}

// TestFTSSearchSpecialCharsDoNotDegrade ensures FTS5 operator/column syntax in
// user input (colon, hyphen, parens, quotes) never escapes into the MATCH
// expression and crashes the source. The source must execute successfully.
func TestFTSSearchSpecialCharsDoNotDegrade(t *testing.T) {
	db := openTestDB(t)
	seedProseChunk(t, db, "/c.md", "server config value pairs and connection settings")

	h := &FTSHandler{DB: db}
	for _, q := range []string{`config: value`, `re-index "now"`, `(retry) OR NOT logic`, `c++ AND go`} {
		resp, err := h.Search(context.Background(), &pb.FTSSearchRequest{Query: q, K: 10})
		if err != nil {
			t.Fatalf("Search(%q): %v", q, err)
		}
		if resp.Status.Status == "DEGRADED" || resp.Status.Status == "CONTRACT_ERROR" {
			t.Errorf("query %q degraded: status=%s err=%s", q, resp.Status.Status, resp.Status.ErrorMessage)
		}
	}
}

// TestFTSSearchVeryLongQueryDoesNotError ensures a pathologically long query is
// bounded (length + term caps) and never crashes or degrades the source —
// guards the UNBOUNDED_MATCH_EXPRESSION resource path.
func TestFTSSearchVeryLongQueryDoesNotError(t *testing.T) {
	db := openTestDB(t)
	seedProseChunk(t, db, "/e.md", "alpha beta gamma delta epsilon content here")

	huge := strings.Repeat("term ", 5000) + "alpha"
	h := &FTSHandler{DB: db}
	resp, err := h.Search(context.Background(), &pb.FTSSearchRequest{Query: huge, K: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if resp.Status.Status == "DEGRADED" || resp.Status.Status == "CONTRACT_ERROR" {
		t.Errorf("oversized query degraded: status=%s err=%s", resp.Status.Status, resp.Status.ErrorMessage)
	}
}

// TestFTSSearchPunctuationOnlyQueryIsEmptyNotError ensures a query with no
// usable terms short-circuits to EMPTY_BUT_EXECUTED rather than issuing a
// malformed MATCH.
func TestFTSSearchPunctuationOnlyQueryIsEmptyNotError(t *testing.T) {
	db := openTestDB(t)
	seedProseChunk(t, db, "/d.md", "some indexed content here")

	h := &FTSHandler{DB: db}
	resp, err := h.Search(context.Background(), &pb.FTSSearchRequest{Query: "?! @#", K: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if resp.Status.Status != "EMPTY_BUT_EXECUTED" {
		t.Errorf("status = %s, want EMPTY_BUT_EXECUTED", resp.Status.Status)
	}
}
