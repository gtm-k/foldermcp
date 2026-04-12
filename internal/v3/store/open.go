//go:build cgo

package store

import (
	"database/sql"
	"errors"
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
// It applies the tier's PRAGMA profile and foreign_keys=ON.
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
			_ = db.Close()
			return nil, fmt.Errorf("%s: %w", p, err)
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

// ensureInstanceUUID reads the instance_uuid from config; if blank,
// generates a fresh v4 UUID, persists it, and returns it. Idempotent —
// subsequent calls return the same UUID for the lifetime of the store.
// Callers must invoke this after Migrate() so the config table exists.
func ensureInstanceUUID(db *sql.DB) (string, error) {
	var existing string
	err := db.QueryRow(`SELECT value FROM config WHERE key='instance_uuid'`).Scan(&existing)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if existing != "" {
		return existing, nil
	}
	id := uuid.NewString()
	if _, err := db.Exec(`INSERT OR REPLACE INTO config(key,value) VALUES('instance_uuid', ?)`, id); err != nil {
		return "", err
	}
	return id, nil
}
