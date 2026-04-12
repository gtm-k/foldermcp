//go:build cgo

package walker

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"strings"
	"time"
)

// Options configures the Walk pass.
type Options struct {
	Root        string
	IgnoreGlobs []string // gitignore-style filename globs
	BatchSize   int
}

// Walk enumerates Root and upserts one row per file into the files table.
// Returns the number of files visited and any error. Directories matching
// common VCS/build patterns (.git, node_modules, etc.) are skipped.
func Walk(ctx context.Context, db *sql.DB, opts Options) (int, error) {
	if opts.BatchSize == 0 {
		opts.BatchSize = 100
	}
	var count int
	var batch []fileRow

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		stmt, err := tx.Prepare(`
INSERT INTO files(path, sha256, size, mtime, mime, content_class, parent_dir, last_seen)
VALUES (?,?,?,?,?,?,?,?)
ON CONFLICT(path) DO UPDATE SET
    sha256=excluded.sha256, size=excluded.size, mtime=excluded.mtime,
    mime=excluded.mime, content_class=excluded.content_class,
    last_seen=excluded.last_seen, deleted_at=NULL`)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		defer func() { _ = stmt.Close() }()

		now := time.Now().Unix()
		for _, r := range batch {
			if _, err := stmt.Exec(r.path, r.sha256, r.size, r.mtime, r.mime, r.class, r.parent, now); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
		batch = batch[:0]
		return tx.Commit()
	}

	err := filepath.WalkDir(opts.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if shouldIgnoreDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if skipFile(d.Name(), opts.IgnoreGlobs) {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}
		sum, err := HashFile(path)
		if err != nil {
			return nil
		}
		mime, class, err := ClassifyFile(path)
		if err != nil {
			return nil
		}

		batch = append(batch, fileRow{
			path:   path,
			sha256: sum,
			size:   info.Size(),
			mtime:  info.ModTime().Unix(),
			mime:   mime,
			class:  class,
			parent: filepath.Dir(path),
		})
		count++

		if len(batch) >= opts.BatchSize {
			return flush()
		}
		return nil
	})
	if err != nil {
		return count, err
	}
	return count, flush()
}

type fileRow struct {
	path   string
	sha256 []byte
	size   int64
	mtime  int64
	mime   string
	class  string
	parent string
}

func shouldIgnoreDir(name string) bool {
	switch name {
	case ".git", ".foldermcp", "node_modules", "__pycache__", ".venv", "venv", "target", "dist", "build":
		return true
	}
	return false
}

func skipFile(name string, globs []string) bool {
	for _, g := range globs {
		if ok, _ := filepath.Match(g, name); ok {
			return true
		}
	}
	return strings.HasPrefix(name, ".") && name != ".env"
}
