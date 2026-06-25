//go:build cgo

package store

import (
	"database/sql"
	"fmt"

	sqlitevec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

// init registers sqlite-vec as an auto-extension so every subsequent
// SQLite3 connection opened anywhere in this process loads it. The
// sqlite-vec-go-bindings library exposes Auto() rather than a per-
// connection LoadExtension() because it wires into SQLite's
// sqlite3_auto_extension() hook, not the per-db extension loader. One
// call is sufficient for the lifetime of the process.
func init() {
	sqlitevec.Auto()
}

type Options struct {
	Path     string
	Tier     Tier
	ReadOnly bool
}

// Open returns a *sql.DB with FTS5 available and sqlite-vec loaded.
// The pool is pinned to a single connection (SetMaxOpenConns(1))
// because SQLite PRAGMAs are per-connection state — without pinning,
// a pooled *sql.DB could serve queries on connections that never had
// foreign_keys=ON or journal_mode=WAL applied.
// sqlite-vec is registered as an auto-extension at package init time,
// so every new connection opened here has it loaded.
func Open(opts Options) (*sql.DB, error) {
	dsn := opts.Path
	if opts.ReadOnly {
		dsn += "?mode=ro"
	}
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("sql.Open: %w", err)
	}
	db.SetMaxOpenConns(1)

	prof, ok := pragmaProfiles[opts.Tier]
	if !ok {
		prof = pragmaProfiles[TierMid]
	}

	// Read-safe PRAGMAs applied on every open (including read-only).
	readPragmas := []string{
		"PRAGMA foreign_keys=ON",
		"PRAGMA temp_store=MEMORY",
		fmt.Sprintf("PRAGMA mmap_size=%d", prof.MmapSize),
		fmt.Sprintf("PRAGMA cache_size=%d", prof.CacheSizePages),
	}
	for _, p := range readPragmas {
		if _, err := db.Exec(p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("%s: %w", p, err)
		}
	}

	// Write-mode PRAGMAs that change file format or affect WAL behavior.
	if !opts.ReadOnly {
		writePragmas := []string{
			"PRAGMA journal_mode=WAL",
			"PRAGMA synchronous=NORMAL",
			"PRAGMA auto_vacuum=INCREMENTAL",
			"PRAGMA wal_autocheckpoint=1000",
			fmt.Sprintf("PRAGMA journal_size_limit=%d", prof.JournalSizeLimit),
		}
		for _, p := range writePragmas {
			if _, err := db.Exec(p); err != nil {
				_ = db.Close()
				return nil, fmt.Errorf("%s: %w", p, err)
			}
		}
	}

	if !opts.ReadOnly {
		if err := initVec(db); err != nil {
			_ = db.Close()
			return nil, err
		}
	}

	return db, nil
}

// initVec creates the vec0 virtual table for int8[384] embeddings.
// Idempotent — safe on reopens. sqlite-vec must already be loaded
// before this is called.
func initVec(db *sql.DB) error {
	const ddl = `
CREATE VIRTUAL TABLE IF NOT EXISTS embeddings USING vec0(
    chunk_id INTEGER PRIMARY KEY,
    embedding int8[384]
);`
	_, err := db.Exec(ddl)
	return err
}

// ensureInstanceUUID atomically populates the instance_uuid config row
// if it is blank, then returns whatever value persisted. Safe against
// concurrent callers: the UPDATE only modifies the row if value is still
// empty, so at most one caller's UUID wins; all callers read back the
// winner. Callers must invoke this after Migrate() so the config table
// exists.
func ensureInstanceUUID(db *sql.DB) (string, error) {
	id := uuid.NewString()
	if _, err := db.Exec(
		`UPDATE config SET value=? WHERE key='instance_uuid' AND value=''`, id,
	); err != nil {
		return "", fmt.Errorf("ensureInstanceUUID update: %w", err)
	}
	var result string
	if err := db.QueryRow(
		`SELECT value FROM config WHERE key='instance_uuid'`,
	).Scan(&result); err != nil {
		return "", fmt.Errorf("ensureInstanceUUID read: %w", err)
	}
	return result, nil
}
