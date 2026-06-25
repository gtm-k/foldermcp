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
