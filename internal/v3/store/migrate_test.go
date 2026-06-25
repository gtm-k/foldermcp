//go:build cgo

package store

import (
	"database/sql"
	"fmt"
	"io/fs"
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

	// Fresh DB: readSchemaVersion returns 0 (no config table yet), pending
	// includes 0001_init + 0002_invalidate_on_sha256 + 0003_widen_quantmode_check,
	// apply all three, schema_version → 3.
	if err := Migrate(db, filepath.Join(tmp, "backup/pre.db")); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	v, err := readSchemaVersion(db)
	if err != nil {
		t.Fatalf("readSchemaVersion: %v", err)
	}
	if v != 3 {
		t.Errorf("schema_version = %d, want 3", v)
	}

	// Second call must be a no-op.
	if err := Migrate(db, ""); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
	v2, _ := readSchemaVersion(db)
	if v2 != 3 {
		t.Errorf("re-migrate moved schema_version to %d", v2)
	}
}

// Migration 0003 widens the embedding_fingerprint.quantization_mode CHECK to
// admit the new "int8_fixed" scheme while keeping the legacy values valid and
// still rejecting unknown modes. SQLite cannot ALTER a CHECK in place, so 0003
// rebuilds the (singleton, FK-free) table — this test guards that rebuild.
func TestMigration0003AllowsInt8FixedQuantMode(t *testing.T) {
	tmp := t.TempDir()
	db, err := Open(Options{Path: filepath.Join(tmp, "q.db"), Tier: TierMid})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := Migrate(db, ""); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	base := Fingerprint{ModelName: "m", ModelVersion: "v", Dimension: 384, ChunkingPolicy: "p"}

	// Scale-tagged fixed modes (any int8_fixed_s<digits>) must be accepted.
	for _, mode := range []string{"int8_fixed_s256", "int8_fixed_s512", "int8", "float32"} {
		f := base
		f.QuantizationMode = mode
		if err := WriteFingerprint(db, f); err != nil {
			t.Errorf("valid mode %q rejected: %v", mode, err)
		}
		if got, _ := ReadFingerprint(db); got.QuantizationMode != mode {
			t.Errorf("QuantizationMode = %q, want %q", got.QuantizationMode, mode)
		}
	}

	// Unknown modes — including a scale-LESS "int8_fixed" — are rejected by the
	// GLOB, so the scale can never silently drop out of the label.
	for _, bad := range []string{"garbage", "int8_fixed", "int8_fixed_s", "int8_fixed_sx"} {
		f := base
		f.QuantizationMode = bad
		if err := WriteFingerprint(db, f); err == nil {
			t.Errorf("expected CHECK to reject quantization_mode %q", bad)
		}
	}
}

// Migration 0003 rebuilds embedding_fingerprint; its only non-trivial job is
// preserving the existing singleton row across the rebuild. Every other test
// migrates a FRESH DB (empty fingerprint), so the INSERT...SELECT copy path is
// never exercised with data — a column-list bug would silently lose a real
// user's fingerprint and pass all of them. This drives the actual upgrade path:
// a populated v2 row must survive to v3 byte-for-byte, or CheckFingerprint
// would hit ErrNoRows, return nil, and serve a stale index without a rebuild.
func TestMigration0003PreservesExistingFingerprintRow(t *testing.T) {
	tmp := t.TempDir()
	db, err := Open(Options{Path: filepath.Join(tmp, "upgrade.db"), Tier: TierMid})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Bring the DB to schema_version=2 WITHOUT 0003: apply 0001+0002 raw, then
	// stamp version 2 so Migrate will apply only 0003.
	mfs, _ := MigrationFiles()
	for _, f := range []string{"0001_init.sql", "0002_invalidate_on_sha256.sql"} {
		raw, rerr := fs.ReadFile(mfs, f)
		if rerr != nil {
			t.Fatalf("read %s: %v", f, rerr)
		}
		if _, eerr := db.Exec(string(raw)); eerr != nil {
			t.Fatalf("apply %s: %v", f, eerr)
		}
	}
	if _, err := db.Exec(`INSERT INTO config(key,value) VALUES('schema_version','2')
		ON CONFLICT(key) DO UPDATE SET value='2'`); err != nil {
		t.Fatalf("seed schema_version: %v", err)
	}

	// Seed a realistic populated fingerprint (the pre-upgrade per-vector index).
	seed := Fingerprint{
		ModelName: "all-MiniLM-L6-v2", ModelVersion: "2.2.0", Dimension: 384,
		ChunkingPolicy: "recursive_80_200_260_v1", QuantizationMode: "int8",
	}
	if err := WriteFingerprint(db, seed); err != nil {
		t.Fatalf("seed fingerprint: %v", err)
	}
	var beforeCreated int64
	if err := db.QueryRow(`SELECT created_at FROM embedding_fingerprint WHERE id=1`).Scan(&beforeCreated); err != nil {
		t.Fatalf("read created_at: %v", err)
	}

	// Upgrade: applies ONLY 0003 (current=2), exercising the row copy.
	if err := Migrate(db, ""); err != nil {
		t.Fatalf("Migrate to 3: %v", err)
	}
	if v, _ := readSchemaVersion(db); v != 3 {
		t.Errorf("schema_version = %d, want 3", v)
	}

	got, err := ReadFingerprint(db)
	if err != nil {
		t.Fatalf("fingerprint LOST across 0003 rebuild: %v", err)
	}
	if got != seed {
		t.Errorf("fingerprint changed across rebuild: got %+v, want %+v", got, seed)
	}
	var afterCreated int64
	if err := db.QueryRow(`SELECT created_at FROM embedding_fingerprint WHERE id=1`).Scan(&afterCreated); err != nil {
		t.Fatalf("read created_at after: %v", err)
	}
	if afterCreated != beforeCreated {
		t.Errorf("created_at not preserved: %d → %d", beforeCreated, afterCreated)
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
	if v != "3" {
		t.Errorf("snapshot schema_version = %q, want 3", v)
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
