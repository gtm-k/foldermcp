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
	// IncludeSecrets disables the default-on secret-hygiene deny list
	// (secretDenyGlobs). Default false: credential-bearing files
	// (.env*, *.pem, *.key, id_rsa*, *.p12, *.pfx, *credentials*, *.keystore)
	// never enter the files table — a matched file is never indexed, so its
	// secret is unfindable (Phase 7 Layer 1, D17). Explicit config may set this
	// true to opt back in.
	IncludeSecrets bool
}

// secretDenyGlobs are filename patterns that, by default, are excluded from the
// walk so credential-bearing files never enter the index (Phase 7 Layer 1, D17).
// .git is already excluded as a directory (shouldIgnoreDir). Overridable via
// Options.IncludeSecrets.
var secretDenyGlobs = []string{
	".env", ".env.*", "*.env",
	"*.pem", "*.key", "id_rsa", "id_rsa.*",
	"*.p12", "*.pfx", "*credentials*", "*.keystore",
}

// matchesSecretDeny reports whether name matches any default secret-deny glob.
func matchesSecretDeny(name string) bool {
	lower := strings.ToLower(name)
	for _, g := range secretDenyGlobs {
		// LOW-7: fail CLOSED on a malformed pattern. filepath.Match only errors on
		// a bad PATTERN (ErrBadPattern), never on the name, so an error means our
		// own deny glob is broken — treat that as a match and exclude the file
		// rather than indexing a credential-bearing file because the guard threw.
		if ok, err := filepath.Match(g, lower); err != nil || ok {
			return true
		}
	}
	return false
}

// Walk enumerates Root and upserts one row per file into the files table.
// Returns the number of files visited and any error. Directories matching
// common VCS/build patterns (.git, node_modules, etc.) are skipped.
//
// Note: Walk does not mark disappeared files as deleted (deleted_at).
// Deletion detection is a separate concern handled by the pipeline
// orchestrator (Phase C pipeline.go), which compares last_seen against
// the current run's timestamp after the walk completes.
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
		// Layer 1 secret hygiene (D17): default-deny credential-bearing files so
		// they never enter the files table — overridable by IncludeSecrets.
		if !opts.IncludeSecrets && matchesSecretDeny(d.Name()) {
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
