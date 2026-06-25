//go:build cgo

package embed

import (
	"path/filepath"
	"testing"

	"github.com/gtm-k/foldermcp/internal/v3/store"
)

func TestSemanticIndexCompatible(t *testing.T) {
	tmp := t.TempDir()
	db, err := store.Open(store.Options{Path: filepath.Join(tmp, "c.db"), Tier: store.TierMid})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := store.Migrate(db, ""); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Fresh index (no fingerprint row yet): compatible, so semantic search
	// stays on until the indexer writes the fingerprint.
	if ok, stored := SemanticIndexCompatible(db); !ok {
		t.Errorf("fresh index: ok=false stored=%q, want compatible", stored)
	}

	// After EnsureFingerprint stamps this binary's scale-tagged mode: compatible.
	if err := NewWriter(db).EnsureFingerprint(); err != nil {
		t.Fatalf("EnsureFingerprint: %v", err)
	}
	if ok, stored := SemanticIndexCompatible(db); !ok {
		t.Errorf("matching mode: ok=false stored=%q, want compatible", stored)
	}

	// An existing per-vector "int8" index: incompatible. The helper returns the
	// stored mode so the caller can report it and disable semantic search
	// rather than compare fixed-scale query codes against per-vector codes.
	fp, _ := store.ReadFingerprint(db)
	fp.QuantizationMode = "int8"
	if err := store.WriteFingerprint(db, fp); err != nil {
		t.Fatalf("write legacy fingerprint: %v", err)
	}
	if ok, stored := SemanticIndexCompatible(db); ok || stored != "int8" {
		t.Errorf("legacy int8 index: ok=%v stored=%q, want ok=false stored=int8", ok, stored)
	}
}

// TestSemanticIndexCompatible_FailsClosedOnReadError (Codex gate, Q1 finding 1):
// a fingerprint READ error that is NOT sql.ErrNoRows (e.g. a missing/corrupt
// embedding_fingerprint table — a schema fault, not a fresh index) must fail
// CLOSED: we cannot prove the stored codes are comparable, so semantic search
// must be disabled rather than run KNN on possibly-incomparable vectors and
// return silent-garbage rankings. Only sql.ErrNoRows (genuinely fresh index)
// may fail open.
func TestSemanticIndexCompatible_FailsClosedOnReadError(t *testing.T) {
	tmp := t.TempDir()
	db, err := store.Open(store.Options{Path: filepath.Join(tmp, "c.db"), Tier: store.TierMid})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := store.Migrate(db, ""); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	// Simulate a schema fault: the fingerprint table is unreadable. ReadFingerprint
	// now returns a "no such table" error, which is NOT sql.ErrNoRows.
	if _, err := db.Exec(`DROP TABLE embedding_fingerprint`); err != nil {
		t.Fatalf("drop fingerprint table: %v", err)
	}
	if ok, stored := SemanticIndexCompatible(db); ok {
		t.Errorf("unreadable fingerprint: ok=true stored=%q, want ok=false (fail closed)", stored)
	}
}
