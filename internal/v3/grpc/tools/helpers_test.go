//go:build cgo

package tools

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/gtm-k/foldermcp/internal/v3/store"
)

// openTestDB opens a temp store with migrations applied.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	tmp := t.TempDir()
	db, err := store.Open(store.Options{Path: filepath.Join(tmp, "test.db"), Tier: store.TierMid})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(db, ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query[:min(len(query), 60)], err)
	}
}
