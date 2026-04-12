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

	return db, nil
}
