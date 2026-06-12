//go:build cgo

package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestMigrateFromEmptyDB(t *testing.T) {
	tmp := t.TempDir()
	db, err := Open(Options{Path: filepath.Join(tmp, "m.db"), Tier: TierMid})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Fresh DB: readSchemaVersion returns 0 (no config table yet),
	// pending includes 0001_init.sql + 0002_invalidate_on_sha256.sql,
	// apply both, schema_version → 2.
	if err := Migrate(db, filepath.Join(tmp, "backup/pre.db")); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	v, err := readSchemaVersion(db)
	if err != nil {
		t.Fatalf("readSchemaVersion: %v", err)
	}
	if v != 2 {
		t.Errorf("schema_version = %d, want 2", v)
	}

	// Second call must be a no-op.
	if err := Migrate(db, ""); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
	v2, _ := readSchemaVersion(db)
	if v2 != 2 {
		t.Errorf("re-migrate moved schema_version to %d", v2)
	}
}

func TestSnapshotCreatesFile(t *testing.T) {
	tmp := t.TempDir()
	db, err := Open(Options{Path: filepath.Join(tmp, "s.db"), Tier: TierMid})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := Migrate(db, ""); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	snap := filepath.Join(tmp, "backup", "pre-v2.db")
	if err := Snapshot(db, snap); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if _, err := os.Stat(snap); err != nil {
		t.Errorf("snapshot file missing: %v", err)
	}

	snapDB, err := sql.Open("sqlite3", snap)
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	defer func() { _ = snapDB.Close() }()
	var v string
	if err := snapDB.QueryRow(`SELECT value FROM config WHERE key='schema_version'`).Scan(&v); err != nil {
		t.Fatalf("snapshot schema_version: %v", err)
	}
	if v != "2" {
		t.Errorf("snapshot schema_version = %q, want 2", v)
	}
}

// TestSha256ChangeFiresInvalidationTrigger gates the entire D28b
// orchestrator (plan §4.1 Layer 1): a walker-style UPSERT that changes a
// file's sha256 must fire the files_sha256_invalidate trigger from
// migration 0002 and atomically clear ALL downstream state — embeddings
// (vec0 virtual table, deleted first while chunk rows still identify the
// orphans; Amendment 2026-06-11), pipeline_state, nodes, chunks (FK
// cascade), and chunks_fts (chunks_ad trigger). If this test fails, the
// idempotence invariant is broken and Runner cannot be trusted,
// regardless of its own tests.
func TestSha256ChangeFiresInvalidationTrigger(t *testing.T) {
	tmp := t.TempDir()
	db, err := Open(Options{Path: filepath.Join(tmp, "trg.db"), Tier: TierMid})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := Migrate(db, ""); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Seed the full chain for one file: files → nodes → chunks →
	// embeddings + pipeline_state.
	if _, err := db.Exec(`INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen) VALUES('/w/a.md',?,10,1,'text/plain','document','/w',1)`, []byte("old-sha")); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO nodes(file_id,node_type,name,provenance,created_at,updated_at) VALUES(1,'file','a.md','EXTRACTED',1,1)`); err != nil {
		t.Fatalf("seed node: %v", err)
	}
	for _, text := range []string{"alpha bravo charlie", "delta echo foxtrot"} {
		if _, err := db.Exec(`INSERT INTO chunks(node_id,text,byte_start,byte_end,token_count,chunk_kind) VALUES(1,?,0,19,3,'prose')`, text); err != nil {
			t.Fatalf("seed chunk: %v", err)
		}
	}

	// Capture the file's chunk_ids before the UPSERT (Amendment item 3).
	var chunkIDs []int64
	rows, err := db.Query(`SELECT chunk_id FROM chunks WHERE node_id=1`)
	if err != nil {
		t.Fatalf("capture chunk ids: %v", err)
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan chunk id: %v", err)
		}
		chunkIDs = append(chunkIDs, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("chunk id rows: %v", err)
	}
	if len(chunkIDs) != 2 {
		t.Fatalf("seeded chunk count = %d, want 2", len(chunkIDs))
	}

	vec := make([]byte, 384)
	for _, id := range chunkIDs {
		if _, err := db.Exec(`INSERT INTO embeddings(chunk_id, embedding) VALUES(?, vec_int8(?))`, id, vec); err != nil {
			t.Fatalf("seed embedding %d: %v", id, err)
		}
	}
	for _, pass := range []string{"walker", "structural", "chunker", "embeddings"} {
		if _, err := db.Exec(`INSERT INTO pipeline_state(file_id,pass_name,status,checkpoint_at) VALUES(1,?,'done',1)`, pass); err != nil {
			t.Fatalf("seed pipeline_state %s: %v", pass, err)
		}
	}

	// Walker-style UPSERT with a different sha256 — the exact statement
	// shape walker.Walk uses, so this test verifies the trigger fires
	// under SQLite's UPSERT (ON CONFLICT DO UPDATE) semantics.
	if _, err := db.Exec(`
INSERT INTO files(path, sha256, size, mtime, mime, content_class, parent_dir, last_seen)
VALUES ('/w/a.md',?,12,2,'text/plain','document','/w',2)
ON CONFLICT(path) DO UPDATE SET
    sha256=excluded.sha256, size=excluded.size, mtime=excluded.mtime,
    mime=excluded.mime, content_class=excluded.content_class,
    last_seen=excluded.last_seen, deleted_at=NULL`, []byte("new-sha")); err != nil {
		t.Fatalf("walker-style upsert: %v", err)
	}

	// 1. The file row shows the new sha256.
	var sha []byte
	if err := db.QueryRow(`SELECT sha256 FROM files WHERE file_id=1`).Scan(&sha); err != nil {
		t.Fatalf("read sha256: %v", err)
	}
	if string(sha) != "new-sha" {
		t.Errorf("sha256 = %q, want new-sha", sha)
	}

	// 2. nodes rows are gone.
	assertCount(t, db, `SELECT COUNT(*) FROM nodes WHERE file_id=1`, 0, "nodes")
	// 3. chunks rows are gone (FK cascade from nodes).
	assertCount(t, db, `SELECT COUNT(*) FROM chunks`, 0, "chunks")
	// 4. chunks_fts MATCH for the old chunk text returns zero hits.
	assertCount(t, db, `SELECT COUNT(*) FROM chunks_fts WHERE chunks_fts MATCH 'bravo'`, 0, "chunks_fts old text")
	// 5. embeddings rows for the captured chunk_ids are gone
	//    (Amendment item 3 — vec0 has no FK cascade, the trigger must
	//    have deleted them explicitly).
	for _, id := range chunkIDs {
		assertCount(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM embeddings WHERE chunk_id = %d`, id), 0, "embeddings")
	}
	assertCount(t, db, `SELECT COUNT(*) FROM embeddings`, 0, "embeddings total")
	// 6. pipeline_state rows for the file are gone.
	assertCount(t, db, `SELECT COUNT(*) FROM pipeline_state WHERE file_id=1`, 0, "pipeline_state")
}

func assertCount(t *testing.T, db *sql.DB, query string, want int, label string) {
	t.Helper()
	var got int
	if err := db.QueryRow(query).Scan(&got); err != nil {
		t.Fatalf("%s count query: %v", label, err)
	}
	if got != want {
		t.Errorf("%s count = %d, want %d", label, got, want)
	}
}
