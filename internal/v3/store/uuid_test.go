//go:build cgo

package store

import (
	"path/filepath"
	"testing"
)

func TestInstanceUUIDStable(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "u.db")

	db, err := Open(Options{Path: p, Tier: TierMid})
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	if err := Migrate(db, ""); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	first, err := ensureInstanceUUID(db)
	if err != nil {
		t.Fatalf("first uuid: %v", err)
	}
	if first == "" {
		t.Fatal("first uuid empty")
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close before reopen: %v", err)
	}

	db2, err := Open(Options{Path: p, Tier: TierMid})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = db2.Close() }()
	second, err := ensureInstanceUUID(db2)
	if err != nil {
		t.Fatalf("second uuid: %v", err)
	}
	if first != second {
		t.Errorf("uuid changed across reopens: %q vs %q", first, second)
	}
}

func TestInstanceUUIDReturnsExistingOnRepeatedCall(t *testing.T) {
	tmp := t.TempDir()
	db, err := Open(Options{Path: filepath.Join(tmp, "u2.db"), Tier: TierMid})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := Migrate(db, ""); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	a, _ := ensureInstanceUUID(db)
	b, _ := ensureInstanceUUID(db)
	if a != b {
		t.Errorf("second call returned new uuid: %q vs %q", a, b)
	}
}
