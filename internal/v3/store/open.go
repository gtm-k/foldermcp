//go:build cgo

package store

import (
	"database/sql"
	"fmt"

	sqlitevec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

type Options struct {
	Path     string
	Tier     Tier
	ReadOnly bool
}

// Open returns a *sql.DB with FTS5 available and sqlite-vec loaded.
// It applies the tier's PRAGMA profile and foreign_keys=ON.
func Open(opts Options) (*sql.DB, error) {
	dsn := opts.Path
	if opts.ReadOnly {
		dsn += "?mode=ro"
	}
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("sql.Open: %w", err)
	}

	if err := sqlitevec.LoadExtension(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("load sqlite-vec: %w", err)
	}

	prof, ok := pragmaProfiles[opts.Tier]
	if !ok {
		prof = pragmaProfiles[TierMid]
	}

	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA auto_vacuum=INCREMENTAL",
		"PRAGMA temp_store=MEMORY",
		"PRAGMA wal_autocheckpoint=1000",
		fmt.Sprintf("PRAGMA mmap_size=%d", prof.MmapSize),
		fmt.Sprintf("PRAGMA cache_size=%d", prof.CacheSizePages),
		fmt.Sprintf("PRAGMA journal_size_limit=%d", prof.JournalSizeLimit),
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("%s: %w", p, err)
		}
	}

	if !opts.ReadOnly {
		if err := initVec(db); err != nil {
			db.Close()
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
