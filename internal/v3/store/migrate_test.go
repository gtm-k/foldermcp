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
	// includes 0001_init + 0002_invalidate_on_sha256 + 0003_widen_quantmode_check
	// + 0004_widen_chunk_kind, apply all four, schema_version → 4.
	if err := Migrate(db, filepath.Join(tmp, "backup/pre.db")); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	v, err := readSchemaVersion(db)
	if err != nil {
		t.Fatalf("readSchemaVersion: %v", err)
	}
	if v != 4 {
		t.Errorf("schema_version = %d, want 4", v)
	}

	// Second call must be a no-op.
	if err := Migrate(db, ""); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
	v2, _ := readSchemaVersion(db)
	if v2 != 4 {
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

	// Upgrade from current=2 applies 0003 (the row copy under test) AND 0004
	// (chunk_kind widening), landing at schema_version 4. 0003 runs first, so
	// the fingerprint copy path this test guards is still exercised; the final
	// version is 4 because the runner applies every pending migration.
	if err := Migrate(db, ""); err != nil {
		t.Fatalf("Migrate to 4: %v", err)
	}
	if v, _ := readSchemaVersion(db); v != 4 {
		t.Errorf("schema_version = %d, want 4", v)
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
	if v != "4" {
		t.Errorf("snapshot schema_version = %q, want 4", v)
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

// allChunkKinds is every kind the 0004-widened CHECK must admit: the M1 kinds
// (already allowed by 0001) plus Program-A's net-new kinds. seedChunkKind below
// inserts one chunk per kind so the upgrade and from-empty tests both prove the
// widened CHECK end-to-end.
var (
	m1ChunkKinds  = []string{"prose", "code_ast", "pdf_text", "code_fallback"}
	newChunkKinds = []string{"office_text", "csv_schema", "csv_rows", "data_structured"}
)

// seedFileNodeChunkEmbedding inserts one file → node → chunk(kind) → embedding
// chain and returns the minted chunk_id, exercising the FK + FTS + vec0 path the
// migration must preserve. text is FTS-indexable so probe queries can hit it.
func seedFileNodeChunkEmbedding(t *testing.T, db *sql.DB, path, kind, text string) int64 {
	t.Helper()
	var fileID int64
	if err := db.QueryRow(`INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
		VALUES(?,?,1,1,'text/plain','document','/w',1) RETURNING file_id`, path, []byte("sha-"+path)).Scan(&fileID); err != nil {
		t.Fatalf("seed file %s: %v", path, err)
	}
	var nodeID int64
	if err := db.QueryRow(`INSERT INTO nodes(file_id,node_type,name,provenance,created_at,updated_at)
		VALUES(?,'file',?,'EXTRACTED',1,1) RETURNING node_id`, fileID, filepath.Base(path)).Scan(&nodeID); err != nil {
		t.Fatalf("seed node %s: %v", path, err)
	}
	var chunkID int64
	if err := db.QueryRow(`INSERT INTO chunks(node_id,text,byte_start,byte_end,token_count,chunk_kind,header)
		VALUES(?,?,0,?,3,?,?) RETURNING chunk_id`, nodeID, text, len(text), kind, "hdr "+kind).Scan(&chunkID); err != nil {
		t.Fatalf("seed chunk %s/%s: %v", path, kind, err)
	}
	vec := make([]byte, 384)
	if _, err := db.Exec(`INSERT INTO embeddings(chunk_id, embedding) VALUES(?, vec_int8(?))`, chunkID, vec); err != nil {
		t.Fatalf("seed embedding %s: %v", path, err)
	}
	return chunkID
}

// TestMigration0004FromEmptyAdmitsNewChunkKinds: applying through 0004 on a
// fresh DB succeeds and the widened CHECK accepts every Program-A chunk_kind
// (new AND pre-existing). This is the from-empty half of R1.
func TestMigration0004FromEmptyAdmitsNewChunkKinds(t *testing.T) {
	tmp := t.TempDir()
	db, err := Open(Options{Path: filepath.Join(tmp, "ck.db"), Tier: TierMid})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := Migrate(db, ""); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if v, _ := readSchemaVersion(db); v != 4 {
		t.Fatalf("schema_version = %d, want 4", v)
	}

	// Every kind — new and pre-existing — must insert without a CHECK failure.
	for i, kind := range append(append([]string{}, m1ChunkKinds...), newChunkKinds...) {
		seedFileNodeChunkEmbedding(t, db, fmt.Sprintf("/w/f%d.txt", i), kind, "fixture text for "+kind)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM chunks`); got != len(m1ChunkKinds)+len(newChunkKinds) {
		t.Errorf("chunk count = %d, want %d", got, len(m1ChunkKinds)+len(newChunkKinds))
	}

	// An unknown kind is still rejected by the widened CHECK.
	var nodeID int64
	if err := db.QueryRow(`SELECT node_id FROM nodes LIMIT 1`).Scan(&nodeID); err != nil {
		t.Fatalf("read a node: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO chunks(node_id,text,byte_start,byte_end,token_count,chunk_kind)
		VALUES(?,'x',0,1,1,'totally_bogus_kind')`, nodeID); err == nil {
		t.Error("expected CHECK to reject an unknown chunk_kind after 0004")
	}
}

// TestMigration0004UpgradeFromPopulatedV3 is the R1 BLOCKING upgrade test. It
// builds a populated schema_version=3 store (Q1's shipped 0003), seeds nodes +
// chunks across kinds + matching embeddings + FTS, THEN applies ONLY 0004 and
// asserts the rebuild is lossless and not silently skipped: identical chunk row
// count, identical chunk_ids (rowid preservation — embeddings.chunk_id and the
// chunks_fts content_rowid depend on it), zero foreign_key_check rows, zero
// orphaned embeddings, FTS integrity passes, identical FTS hit-set for 3 probe
// queries, and the new kinds become insertable. A duplicate-0003 file would pass
// the from-empty test above yet FAIL here by leaving schema_version at 3.
func TestMigration0004UpgradeFromPopulatedV3(t *testing.T) {
	tmp := t.TempDir()
	db, err := Open(Options{Path: filepath.Join(tmp, "upgrade4.db"), Tier: TierMid})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Bring the DB to schema_version=3 WITHOUT 0004: apply 0001+0002+0003 raw,
	// then stamp version 3 so Migrate applies only 0004 (proves the prefix>current
	// selector actually picks 0004 up on a real upgraded store — R1).
	mfs, _ := MigrationFiles()
	for _, f := range []string{"0001_init.sql", "0002_invalidate_on_sha256.sql", "0003_widen_quantmode_check.sql"} {
		raw, rerr := fs.ReadFile(mfs, f)
		if rerr != nil {
			t.Fatalf("read %s: %v", f, rerr)
		}
		if _, eerr := db.Exec(string(raw)); eerr != nil {
			t.Fatalf("apply %s: %v", f, eerr)
		}
	}
	if _, err := db.Exec(`INSERT INTO config(key,value) VALUES('schema_version','3')
		ON CONFLICT(key) DO UPDATE SET value='3'`); err != nil {
		t.Fatalf("seed schema_version: %v", err)
	}

	// Seed several files across the pre-0004 kinds with matching embeddings + FTS.
	// Distinct probe tokens per chunk let us assert FTS hit-set parity.
	seeds := []struct{ path, kind, text string }{
		{"/w/a.md", "prose", "alpha pooling connection prose"},
		{"/w/b.go", "code_ast", "bravo function signature code"},
		{"/w/c.pdf", "pdf_text", "charlie invoice page extracted"},
		{"/w/d.txt", "code_fallback", "delta fallback miscellaneous text"},
	}
	wantChunks := make(map[int64]string, len(seeds))
	for _, s := range seeds {
		id := seedFileNodeChunkEmbedding(t, db, s.path, s.kind, s.text)
		wantChunks[id] = s.text
	}

	// Capture pre-upgrade invariants.
	beforeCount := countRows(t, db, `SELECT COUNT(*) FROM chunks`)
	beforeEmb := countRows(t, db, `SELECT COUNT(*) FROM embeddings`)
	// F10: capture the ORDERED rowid set per probe, not just the hit count — equal
	// counts with different rowids (a rebuild that re-pointed FTS at wrong chunks)
	// would slip past a count-only check.
	probes := []string{"alpha", "charlie", "delta"}
	beforeHitRowids := make(map[string][]int64, len(probes))
	for _, p := range probes {
		beforeHitRowids[p] = ftsHitRowids(t, db, p)
		if len(beforeHitRowids[p]) == 0 {
			t.Fatalf("pre-upgrade probe %q had 0 FTS hits — bad fixture", p)
		}
	}

	// F8: seed ONE genuine orphan embedding — a chunk_id that does NOT exist in
	// chunks — so the migration's D13 orphan-sweep DELETE is actually exercised.
	// Without a planted orphan the "zero orphaned embeddings" assertion is vacuous
	// (the seeded chain never produces one). 999999 is far above any minted
	// chunk_id (the seeds above use small autoincrement ids).
	const orphanChunkID = 999999
	if got := countRows(t, db, `SELECT COUNT(*) FROM chunks WHERE chunk_id=?`, orphanChunkID); got != 0 {
		t.Fatalf("precondition: chunk_id %d unexpectedly exists", orphanChunkID)
	}
	if _, err := db.Exec(`INSERT INTO embeddings(chunk_id, embedding) VALUES(?, vec_int8(?))`, orphanChunkID, make([]byte, 384)); err != nil {
		t.Fatalf("seed orphan embedding: %v", err)
	}
	// embeddings now = beforeEmb legit + 1 orphan; the migration must delete the
	// orphan and keep every legit row.
	legitEmb := beforeEmb
	if got := countRows(t, db, `SELECT COUNT(*) FROM embeddings`); got != legitEmb+1 {
		t.Fatalf("after seeding orphan: embeddings = %d, want %d", got, legitEmb+1)
	}

	// Upgrade: applies ONLY 0004 (current=3), exercising the chunks rebuild.
	if err := Migrate(db, ""); err != nil {
		t.Fatalf("Migrate to 4: %v", err)
	}
	if v, _ := readSchemaVersion(db); v != 4 {
		t.Fatalf("schema_version = %d, want 4 — 0004 was SILENTLY SKIPPED (R1)", v)
	}

	// Identical chunk row COUNT.
	if got := countRows(t, db, `SELECT COUNT(*) FROM chunks`); got != beforeCount {
		t.Errorf("chunk count changed across 0004: %d → %d", beforeCount, got)
	}
	// Identical chunk_ids + text (rowid preservation).
	rows, err := db.Query(`SELECT chunk_id, text FROM chunks`)
	if err != nil {
		t.Fatalf("read chunks after: %v", err)
	}
	gotChunks := make(map[int64]string)
	for rows.Next() {
		var id int64
		var text string
		if err := rows.Scan(&id, &text); err != nil {
			t.Fatalf("scan chunk: %v", err)
		}
		gotChunks[id] = text
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("chunk rows: %v", err)
	}
	if len(gotChunks) != len(wantChunks) {
		t.Errorf("chunk_id set size %d, want %d", len(gotChunks), len(wantChunks))
	}
	for id, want := range wantChunks {
		if got, ok := gotChunks[id]; !ok {
			t.Errorf("chunk_id %d LOST across rebuild (rowid not preserved)", id)
		} else if got != want {
			t.Errorf("chunk_id %d text changed: %q → %q", id, want, got)
		}
	}
	// Zero foreign_key_check rows.
	fkRows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatalf("foreign_key_check: %v", err)
	}
	fkViolations := 0
	for fkRows.Next() {
		fkViolations++
	}
	_ = fkRows.Close()
	if fkViolations != 0 {
		t.Errorf("foreign_key_check returned %d rows, want 0", fkViolations)
	}
	// F8: the planted orphan must be GONE (the D13 sweep's one deleting statement
	// actually ran), and every LEGITIMATE embedding must survive — surviving count
	// == legitEmb exactly, the planted orphan being the only deletion.
	if got := countRows(t, db, `SELECT COUNT(*) FROM embeddings WHERE chunk_id=?`, orphanChunkID); got != 0 {
		t.Errorf("planted orphan embedding (chunk_id %d) survived 0004 — orphan sweep did not run", orphanChunkID)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM embeddings WHERE chunk_id NOT IN (SELECT chunk_id FROM chunks)`); got != 0 {
		t.Errorf("orphaned embeddings = %d, want 0", got)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM embeddings`); got != legitEmb {
		t.Errorf("legit embedding count not preserved across 0004: %d, want %d (orphan should be the only deletion)", got, legitEmb)
	}
	// FTS integrity-check passes (raises on corruption).
	if _, err := db.Exec(`INSERT INTO chunks_fts(chunks_fts, rank) VALUES('integrity-check', 1)`); err != nil {
		t.Errorf("FTS integrity-check failed after 0004: %v", err)
	}
	// F10: identical FTS hit-set (ordered rowid slice) for the probe queries —
	// not just equal counts. Wrong rowids with the same count would fail here.
	for _, p := range probes {
		got := ftsHitRowids(t, db, p)
		if !equalInt64Slices(got, beforeHitRowids[p]) {
			t.Errorf("FTS rowid set for %q changed across rebuild: %v → %v", p, beforeHitRowids[p], got)
		}
	}
	// The new kinds are now insertable (the whole point of the migration).
	for i, kind := range newChunkKinds {
		seedFileNodeChunkEmbedding(t, db, fmt.Sprintf("/w/new%d.txt", i), kind, "post-upgrade "+kind)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM chunks WHERE chunk_kind IN ('office_text','csv_schema','csv_rows','data_structured')`); got != len(newChunkKinds) {
		t.Errorf("new-kind chunks = %d, want %d", got, len(newChunkKinds))
	}
}

// ftsHitRowids returns the rowids matching an FTS probe term in ascending order
// (F10) so the migration test can compare the exact hit-SET pre/post-rebuild,
// not merely the hit count.
func ftsHitRowids(t *testing.T, db *sql.DB, term string) []int64 {
	t.Helper()
	rows, err := db.Query(`SELECT rowid FROM chunks_fts WHERE chunks_fts MATCH ? ORDER BY rowid`, term)
	if err != nil {
		t.Fatalf("fts rowids for %q: %v", term, err)
	}
	defer func() { _ = rows.Close() }()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan fts rowid for %q: %v", term, err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("fts rowid rows for %q: %v", term, err)
	}
	return out
}

func equalInt64Slices(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func countRows(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
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
