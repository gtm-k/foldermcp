//go:build cgo

package store

import (
	"database/sql"
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
	// pending includes 0001_init.sql, apply it, schema_version → 1.
	if err := Migrate(db, filepath.Join(tmp, "backup/pre.db")); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	v, err := readSchemaVersion(db)
	if err != nil {
		t.Fatalf("readSchemaVersion: %v", err)
	}
	if v != 1 {
		t.Errorf("schema_version = %d, want 1", v)
	}

	// Second call must be a no-op.
	if err := Migrate(db, ""); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
	v2, _ := readSchemaVersion(db)
	if v2 != 1 {
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
	if v != "1" {
		t.Errorf("snapshot schema_version = %q, want 1", v)
	}
}
