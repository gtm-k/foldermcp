//go:build cgo

package store

import (
	"database/sql"
	"io/fs"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestMigrationFilesEmbedded(t *testing.T) {
	mfs, err := MigrationFiles()
	if err != nil {
		t.Fatalf("MigrationFiles: %v", err)
	}
	entries, err := fs.ReadDir(mfs, ".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no migrations embedded")
	}
	for _, e := range entries {
		if e.Name() == "0001_init.sql" {
			return
		}
	}
	t.Fatal("0001_init.sql not found in embed.FS")
}

func TestInitialMigrationApplies(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:?_fk=on")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	mfs, _ := MigrationFiles()
	raw, err := fs.ReadFile(mfs, "0001_init.sql")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if _, err := db.Exec(string(raw)); err != nil {
		t.Fatalf("apply 0001: %v", err)
	}

	var v string
	if err := db.QueryRow("SELECT value FROM config WHERE key='schema_version'").Scan(&v); err != nil {
		t.Fatalf("schema_version: %v", err)
	}
	if v != "1" {
		t.Fatalf("schema_version = %q, want 1", v)
	}

	expected := []string{
		"config", "passes", "files", "nodes", "edges", "chunks",
		"chunks_fts", "communities", "blobs", "blob_refs",
		"pipeline_state", "embedding_fingerprint", "llm_enrichments",
	}
	for _, name := range expected {
		var got string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type IN ('table','view') AND name=?",
			name,
		).Scan(&got)
		if err != nil {
			t.Errorf("table %s missing: %v", name, err)
		}
	}
}
