//go:build cgo

package store

import (
	"path/filepath"
	"testing"
)

func TestOpenAppliesPragmas(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")

	db, err := Open(Options{Path: dbPath, Tier: TierMid})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}

	var fkOn int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&fkOn); err != nil {
		t.Fatalf("foreign_keys: %v", err)
	}
	if fkOn != 1 {
		t.Errorf("foreign_keys = %d, want 1", fkOn)
	}
}

func TestOpenLoadsSqliteVec(t *testing.T) {
	tmp := t.TempDir()
	db, err := Open(Options{Path: filepath.Join(tmp, "t.db"), Tier: TierMid})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// vec_version() is a function exported by sqlite-vec
	var version string
	if err := db.QueryRow("SELECT vec_version()").Scan(&version); err != nil {
		t.Fatalf("vec_version: %v (sqlite-vec not loaded?)", err)
	}
	if version == "" {
		t.Error("vec_version() returned empty string")
	}
}

func TestDetectTierBoundaries(t *testing.T) {
	cases := []struct {
		ramMB int
		want  Tier
	}{
		{1024, TierEntry}, {2048, TierEntry}, {2049, TierMid},
		{8192, TierMid}, {8193, TierProsumer}, {32768, TierProsumer},
	}
	for _, c := range cases {
		got := DetectTier(c.ramMB)
		if got != c.want {
			t.Errorf("DetectTier(%d) = %s, want %s", c.ramMB, got, c.want)
		}
	}
}
