//go:build cgo

package store

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

// Migrate applies all pending forward migrations.
// Before applying, it takes a snapshot via Snapshot(db, snapshotPath) if
// snapshotPath != "" AND the current schema_version > 0. A fresh DB
// (schema_version=0) does not get a pre-migration snapshot because
// there is nothing to back up.
//
// It reads schema_version from config (treating ErrNoRows as version 0),
// applies migrations whose numeric prefix > version, and updates
// schema_version atomically per migration transaction.
func Migrate(db *sql.DB, snapshotPath string) error {
	mfs, err := MigrationFiles()
	if err != nil {
		return err
	}

	current, err := readSchemaVersion(db)
	if err != nil {
		return fmt.Errorf("read schema_version: %w", err)
	}

	pending, err := listPendingMigrations(mfs, current)
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}

	if snapshotPath != "" && current > 0 {
		if err := Snapshot(db, snapshotPath); err != nil {
			return fmt.Errorf("pre-migration snapshot: %w", err)
		}
	}

	for _, m := range pending {
		raw, err := fs.ReadFile(mfs, m.filename)
		if err != nil {
			return fmt.Errorf("read %s: %w", m.filename, err)
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(raw)); err != nil {
			_ = tx.Rollback()
			resetLegacyAlterTable(db)
			return fmt.Errorf("apply %s: %w", m.filename, err)
		}
		if _, err := tx.Exec(
			`UPDATE config SET value=? WHERE key='schema_version'`, strconv.Itoa(m.version),
		); err != nil {
			_ = tx.Rollback()
			resetLegacyAlterTable(db)
			return fmt.Errorf("update schema_version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			resetLegacyAlterTable(db)
			return fmt.Errorf("commit %s: %w", m.filename, err)
		}
		// F9: PRAGMA legacy_alter_table is connection-scoped and survives ROLLBACK
		// (and, like all connection PRAGMAs, persists past COMMIT). Migration 0004
		// turns it ON for its rename and turns it OFF again in-SQL on the success
		// path (defense in depth). But if a migration failed mid-flight — or a
		// future migration toggles it and forgets to reset — the protective modern
		// rename semantics would leak to every later statement on this pooled
		// connection (store.Open pins the pool to ONE connection). Reset it to OFF
		// after EVERY migration regardless of outcome so the state never leaks.
		resetLegacyAlterTable(db)
	}
	return nil
}

// resetLegacyAlterTable forces PRAGMA legacy_alter_table=OFF on the connection
// (F9). It is connection-scoped and survives both ROLLBACK and COMMIT, so the
// migration runner resets it on every exit path; the pool is pinned to one
// connection (store.Open) so this reliably clears the one connection migrations
// run on. Best-effort: a failure here is logged-by-return only at callers that
// already have an error, so it never masks the original migration error.
func resetLegacyAlterTable(db *sql.DB) {
	_, _ = db.Exec(`PRAGMA legacy_alter_table=OFF`)
}

// readSchemaVersion returns 0 if the config table does not exist or has no
// schema_version row, so a fresh DB successfully bootstraps via 0001_init.
// Uses sqlite_master to detect table existence rather than matching driver
// error strings, which would be fragile across SQLite versions.
func readSchemaVersion(db *sql.DB) (int, error) {
	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='config'`,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("check config table: %w", err)
	}
	if count == 0 {
		return 0, nil
	}

	var s string
	err := db.QueryRow(`SELECT value FROM config WHERE key='schema_version'`).Scan(&s)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if s == "" {
		return 0, nil
	}
	return strconv.Atoi(s)
}

type migration struct {
	version  int
	filename string
}

func listPendingMigrations(mfs fs.FS, current int) ([]migration, error) {
	entries, err := fs.ReadDir(mfs, ".")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		numStr, _, ok := strings.Cut(name, "_")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(numStr)
		if err != nil {
			continue
		}
		if n > current {
			out = append(out, migration{n, name})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}
