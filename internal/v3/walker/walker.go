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

// MatchesSecretDeny reports whether a filename matches any default secret-deny
// glob (.env, .env.*, *.env, *.pem, *.key, id_rsa*, *.p12, *.pfx, *credentials*,
// *.keystore). Exported so the watch path (pipeline.upsertCandidate) applies the
// SAME Layer-1 deny as the one-shot Walk — a single source of truth, no replicated
// copy. See ShouldSkip for the full per-path predicate.
func MatchesSecretDeny(name string) bool { return matchesSecretDeny(name) }

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

// ShouldSkip is the single source of truth for whether the indexer must NOT
// create a files row for path (relative to root). It applies EVERY rule Walk
// applies, in the same order:
//
//   - any DIRECTORY segment of path whose name is in shouldIgnoreDir
//     (.git, node_modules, build dirs, …) ⇒ skip (Walk prunes the subtree);
//   - the secretDenyGlobs / matchesSecretDeny check on the basename
//     (.env, *.pem, *.key, id_rsa*, *credentials*, …) unless includeSecrets
//     ⇒ skip (Phase 7 Layer 1, D17 — credential-bearing files never index);
//   - the skipFile dotfile/IgnoreGlobs rule on the basename ⇒ skip.
//
// Both Walk (per visited entry) and the watch path (pipeline.upsertCandidate,
// per changed file) call this ONE predicate so the live-edit path can never
// diverge from the one-shot walk. A path outside root (Rel fails or escapes)
// is treated as skip — the indexer only owns paths under root.
//
// includeSecrets mirrors Options.IncludeSecrets: when true the secret-deny
// check is disabled (explicit opt-in). ignoreGlobs mirrors Options.IgnoreGlobs.
func ShouldSkip(root, path string, includeSecrets bool, ignoreGlobs []string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return true
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return true
	}
	segs := strings.Split(rel, "/")
	// All segments except the last are directory components.
	for _, dir := range segs[:len(segs)-1] {
		if shouldIgnoreDir(dir) {
			return true
		}
	}
	base := segs[len(segs)-1]
	// Layer 1 secret hygiene (D17): default-deny credential-bearing files.
	if !includeSecrets && matchesSecretDeny(base) {
		return true
	}
	return skipFile(base, ignoreGlobs)
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
			// Prune ignored subtrees up front (SkipDir avoids descending). The
			// per-file ShouldSkip below re-checks ancestor segments, so this is an
			// optimization, not the authoritative guard.
			if shouldIgnoreDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		// Single source of truth: secret-deny (Layer 1, D17, overridable by
		// IncludeSecrets) + dotfile/IgnoreGlobs rule. ShouldSkip is the SAME
		// predicate the watch path (pipeline.upsertCandidate) calls, so the live
		// path can never diverge from this one-shot walk.
		if ShouldSkip(opts.Root, path, opts.IncludeSecrets, opts.IgnoreGlobs) {
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

// ignoreDirNames is the directory-name ignore set Walk prunes via SkipDir.
// Single source of truth for both shouldIgnoreDir and IgnoreDirNames (the watch
// path's belt-#1 watcher excludes derive from it, so they cannot drift).
var ignoreDirNames = []string{
	".git", ".foldermcp", "node_modules", "__pycache__", ".venv", "venv", "target", "dist", "build",
}

// IgnoreDirNames returns the directory names Walk prunes (.git, node_modules,
// build dirs, …). Exported so the watch path forwards the SAME set to the
// underlying poller as watcher excludes (belt #1) without a replicated copy.
func IgnoreDirNames() []string {
	out := make([]string, len(ignoreDirNames))
	copy(out, ignoreDirNames)
	return out
}

func shouldIgnoreDir(name string) bool {
	for _, n := range ignoreDirNames {
		if name == n {
			return true
		}
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
