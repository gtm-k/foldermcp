//go:build cgo

package embed

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/gtm-k/foldermcp/internal/v3/store"
)

func TestWriterEnforcesFingerprint(t *testing.T) {
	tmp := t.TempDir()
	db, err := store.Open(store.Options{Path: filepath.Join(tmp, "e.db"), Tier: store.TierMid})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := store.Migrate(db, ""); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	w := NewWriter(db)
	if err := w.EnsureFingerprint(); err != nil {
		t.Fatalf("first ensure: %v", err)
	}

	// Verify fingerprint was written
	fp, err := store.ReadFingerprint(db)
	if err != nil {
		t.Fatalf("ReadFingerprint: %v", err)
	}
	if fp.ModelName != ModelName {
		t.Errorf("model = %q, want %q", fp.ModelName, ModelName)
	}

	// Overwrite with a mismatched fingerprint
	if err := store.WriteFingerprint(db, store.Fingerprint{
		ModelName: "different", ModelVersion: "x", Dimension: 384,
		ChunkingPolicy: "recursive_180_220_v1", QuantizationMode: "int8",
	}); err != nil {
		t.Fatalf("overwrite fingerprint: %v", err)
	}

	err = w.EnsureFingerprint()
	if !errors.Is(err, store.ErrFingerprintMismatch) {
		t.Errorf("expected fingerprint mismatch, got %v", err)
	}
}

// The fixed-scale quantization scheme is a different int8 representation than
// the old per-vector scheme, so the writer must stamp QuantizationMode as
// "int8_fixed". This is what makes an existing per-vector index fail the
// startup fingerprint check and get rebuilt. Verified through the public
// round-trip (no test-only accessor on Writer).
func TestEnsureFingerprintUsesInt8Fixed(t *testing.T) {
	tmp := t.TempDir()
	db, err := store.Open(store.Options{Path: filepath.Join(tmp, "fp.db"), Tier: store.TierMid})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := store.Migrate(db, ""); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	w := NewWriter(db)
	if err := w.EnsureFingerprint(); err != nil {
		t.Fatalf("EnsureFingerprint: %v", err)
	}
	fp, err := store.ReadFingerprint(db)
	if err != nil {
		t.Fatalf("ReadFingerprint: %v", err)
	}
	if fp.QuantizationMode != QuantizationModeString() {
		t.Errorf("QuantizationMode = %q, want %q — the fixed-scale scheme must "+
			"stamp the scale-tagged mode so per-vector indexes rebuild",
			fp.QuantizationMode, QuantizationModeString())
	}
	if fp.QuantizationMode == "int8" {
		t.Error("QuantizationMode must not be the legacy per-vector \"int8\"")
	}
}

// Directly pins this change's reason-for-being: a store stamped with the legacy
// per-vector "int8" mode must mismatch when this binary wants the fixed-scale
// scale-tagged mode, surfacing ErrFingerprintMismatch (which the runner treats
// as fatal). TestWriterEnforcesFingerprint only exercises a ModelName mismatch,
// so it would pass even if the int8→int8_fixed delta were ignored.
func TestExistingInt8IndexTriggersRebuild(t *testing.T) {
	tmp := t.TempDir()
	db, err := store.Open(store.Options{Path: filepath.Join(tmp, "old.db"), Tier: store.TierMid})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := store.Migrate(db, ""); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Seed a fingerprint identical to this binary's EXCEPT the legacy mode.
	if err := NewWriter(db).EnsureFingerprint(); err != nil {
		t.Fatalf("seed ensure: %v", err)
	}
	fp, err := store.ReadFingerprint(db)
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	fp.QuantizationMode = "int8" // pretend this index was built per-vector
	if err := store.WriteFingerprint(db, fp); err != nil {
		t.Fatalf("write legacy fingerprint: %v", err)
	}

	// A fresh writer (wanting the scale-tagged mode) must refuse.
	if err := NewWriter(db).EnsureFingerprint(); !errors.Is(err, store.ErrFingerprintMismatch) {
		t.Errorf("EnsureFingerprint on a legacy int8 index = %v, want ErrFingerprintMismatch", err)
	}
}

func TestWriterWriteBatch(t *testing.T) {
	tmp := t.TempDir()
	db, err := store.Open(store.Options{Path: filepath.Join(tmp, "e.db"), Tier: store.TierMid})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := store.Migrate(db, ""); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Seed the FK chain: file → node → chunk
	if _, err := db.Exec(`INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen) VALUES('/x',?,1,1,'text/plain','document','/',1)`, []byte("xx")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO nodes(file_id,node_type,name,provenance,created_at,updated_at) VALUES(1,'doc','x','EXTRACTED',1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO chunks(node_id,text,byte_start,byte_end,token_count,chunk_kind) VALUES(1,'hello',0,5,1,'prose')`); err != nil {
		t.Fatal(err)
	}

	w := NewWriter(db)
	if err := w.EnsureFingerprint(); err != nil {
		t.Fatal(err)
	}

	vec := make([]int8, Dimension)
	for i := range vec {
		vec[i] = int8(i % 64)
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := w.WriteBatch(tx, []EmbedPair{{ChunkID: 1, Vector: vec}}); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// Verify roundtrip
	var got []byte
	if err := db.QueryRow(`SELECT embedding FROM embeddings WHERE chunk_id=1`).Scan(&got); err != nil {
		t.Fatalf("read embedding: %v", err)
	}
	if len(got) != Dimension {
		t.Errorf("embedding len = %d, want %d", len(got), Dimension)
	}
}

func TestWriterRejectsBadDimension(t *testing.T) {
	tmp := t.TempDir()
	db, err := store.Open(store.Options{Path: filepath.Join(tmp, "e.db"), Tier: store.TierMid})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := store.Migrate(db, ""); err != nil {
		t.Fatal(err)
	}

	w := NewWriter(db)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	err = w.WriteBatch(tx, []EmbedPair{{ChunkID: 1, Vector: make([]int8, 100)}})
	if err == nil {
		t.Error("expected error for wrong dimension, got nil")
	}
}
