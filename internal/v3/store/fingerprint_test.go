//go:build cgo

package store

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
)

func TestFingerprintRoundtripAndMismatch(t *testing.T) {
	tmp := t.TempDir()
	db, err := Open(Options{Path: filepath.Join(tmp, "f.db"), Tier: TierMid})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	mfs, _ := MigrationFiles()
	raw, _ := fs.ReadFile(mfs, "0001_init.sql")
	if _, err := db.Exec(string(raw)); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	fp := Fingerprint{
		ModelName:        "all-MiniLM-L6-v2",
		ModelVersion:     "2.2.0",
		Dimension:        384,
		ChunkingPolicy:   "recursive_180_220",
		QuantizationMode: "int8",
	}
	if err := WriteFingerprint(db, fp); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadFingerprint(db)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != fp {
		t.Errorf("roundtrip: got %+v want %+v", got, fp)
	}

	if err := CheckFingerprint(db, fp); err != nil {
		t.Errorf("CheckFingerprint should match: %v", err)
	}

	diff := fp
	diff.QuantizationMode = "float32"
	if err := CheckFingerprint(db, diff); !errors.Is(err, ErrFingerprintMismatch) {
		t.Errorf("expected ErrFingerprintMismatch, got %v", err)
	}
}
