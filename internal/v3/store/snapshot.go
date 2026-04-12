//go:build cgo

package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
)

// Snapshot writes a VACUUM INTO copy of db at snapshotPath.
// The parent directory is created if missing (mode 0700).
func Snapshot(db *sql.DB, snapshotPath string) error {
	if err := os.MkdirAll(filepath.Dir(snapshotPath), 0o700); err != nil {
		return fmt.Errorf("mkdir snapshot dir: %w", err)
	}
	// VACUUM INTO requires the destination file not to exist
	if _, err := os.Stat(snapshotPath); err == nil {
		if err := os.Remove(snapshotPath); err != nil {
			return fmt.Errorf("remove stale snapshot: %w", err)
		}
	}
	_, err := db.Exec("VACUUM INTO ?", snapshotPath)
	return err
}
