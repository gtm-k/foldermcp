//go:build cgo

package walker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gtm-k/foldermcp/internal/v3/store"
)

// TestMatchesSecretDeny_FailsClosedOnBadPattern (LOW-7): filepath.Match returns
// ErrBadPattern on a malformed glob. matchesSecretDeny must treat that error as a
// MATCH (fail closed) so a credential-bearing file is never indexed because the
// guard's own pattern is broken — rather than the previous `if ok, _ := ...; ok`
// which failed OPEN (indexed the file).
func TestMatchesSecretDeny_FailsClosedOnBadPattern(t *testing.T) {
	orig := secretDenyGlobs
	t.Cleanup(func() { secretDenyGlobs = orig })
	// "[" is an unterminated character class — filepath.Match returns ErrBadPattern.
	secretDenyGlobs = []string{"["}
	if !matchesSecretDeny("anything.txt") {
		t.Error("matchesSecretDeny returned false on a malformed glob — must fail CLOSED (exclude the file)")
	}
}

// TestValidateIgnoreGlobs checks the upfront config validation helper: valid
// globs return nil, a malformed glob ("[" — unterminated char class) returns a
// wrapped filepath.ErrBadPattern naming the bad pattern.
func TestValidateIgnoreGlobs(t *testing.T) {
	if err := ValidateIgnoreGlobs([]string{"*.tmp", "vendor/*", "a?b"}); err != nil {
		t.Errorf("ValidateIgnoreGlobs(valid) = %v, want nil", err)
	}
	if err := ValidateIgnoreGlobs(nil); err != nil {
		t.Errorf("ValidateIgnoreGlobs(nil) = %v, want nil", err)
	}
	err := ValidateIgnoreGlobs([]string{"*.tmp", "["})
	if err == nil {
		t.Fatal("ValidateIgnoreGlobs([\"[\"]) = nil, want error on the malformed glob")
	}
	if !errors.Is(err, filepath.ErrBadPattern) {
		t.Errorf("error = %v, want wrapped filepath.ErrBadPattern", err)
	}
	if !strings.Contains(err.Error(), "[") {
		t.Errorf("error %q should name the bad pattern %q", err.Error(), "[")
	}
}

// TestWalkRejectsMalformedIgnoreGlob (MEDIUM fail-open): a malformed user
// IgnoreGlob is a CONFIG ERROR. Walk must validate up front and RETURN the error
// rather than silently dropping it and indexing files the user tried to ignore
// (fail-open) — and rather than indexing nothing (naive fail-closed). Fails first
// on the old code, where skipFile dropped filepath.Match's ErrBadPattern.
func TestWalkRejectsMalformedIgnoreGlob(t *testing.T) {
	walkRoot := t.TempDir()
	dbDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(walkRoot, "secret.tmp"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	db, err := store.Open(store.Options{Path: filepath.Join(dbDir, "w.db"), Tier: store.TierMid})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := store.Migrate(db, ""); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// "[" is an unterminated character class — filepath.Match returns ErrBadPattern.
	_, err = Walk(context.Background(), db, Options{Root: walkRoot, IgnoreGlobs: []string{"["}})
	if err == nil {
		t.Fatal("Walk with a malformed IgnoreGlob returned nil — must surface the config error, not silently mis-index")
	}
	if !errors.Is(err, filepath.ErrBadPattern) {
		t.Errorf("Walk error = %v, want wrapped filepath.ErrBadPattern", err)
	}
	// And it must NOT have silently indexed anything.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM files`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("file count = %d after Walk rejected a bad glob, want 0 (no silent indexing)", count)
	}
}

// TestSkipFileFailsClosedOnBadPattern: even a caller that bypasses upfront
// validation must not fail OPEN. skipFile (via ShouldSkip) must treat a malformed
// glob's ErrBadPattern as a match (skip the file), matching matchesSecretDeny.
func TestSkipFileFailsClosedOnBadPattern(t *testing.T) {
	root := filepath.FromSlash("/root")
	path := filepath.Join(root, "anything.txt")
	if !ShouldSkip(root, path, true, []string{"["}) {
		t.Error("ShouldSkip with a malformed ignore glob returned false — skipFile must fail CLOSED (skip the file)")
	}
}

func TestWalkerUpsertsFiles(t *testing.T) {
	walkRoot := t.TempDir()
	dbDir := t.TempDir() // separate dir for DB to avoid walking WAL/SHM files

	// Seed files
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(walkRoot, "src"), 0755))
	must(os.WriteFile(filepath.Join(walkRoot, "src", "a.py"), []byte("print('hi')"), 0644))
	must(os.WriteFile(filepath.Join(walkRoot, "src", "b.go"), []byte("package main"), 0644))
	must(os.WriteFile(filepath.Join(walkRoot, "README.md"), []byte("# hi"), 0644))
	// ignored dir
	must(os.MkdirAll(filepath.Join(walkRoot, ".git"), 0755))
	must(os.WriteFile(filepath.Join(walkRoot, ".git", "HEAD"), []byte("ref: x"), 0644))

	dbPath := filepath.Join(dbDir, "w.db")
	db, err := store.Open(store.Options{Path: dbPath, Tier: store.TierMid})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := store.Migrate(db, ""); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	n, err := Walk(context.Background(), db, Options{Root: walkRoot})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if n != 3 {
		t.Errorf("walked %d files, want 3 (.git excluded)", n)
	}

	// Verify classes
	var pyClass, goClass, mdClass string
	_ = db.QueryRow(`SELECT content_class FROM files WHERE path=?`, filepath.Join(walkRoot, "src", "a.py")).Scan(&pyClass)
	_ = db.QueryRow(`SELECT content_class FROM files WHERE path=?`, filepath.Join(walkRoot, "src", "b.go")).Scan(&goClass)
	_ = db.QueryRow(`SELECT content_class FROM files WHERE path=?`, filepath.Join(walkRoot, "README.md")).Scan(&mdClass)

	if pyClass != "code" {
		t.Errorf("a.py class = %s, want code", pyClass)
	}
	if goClass != "code" {
		t.Errorf("b.go class = %s, want code", goClass)
	}
	if mdClass != "document" {
		t.Errorf("README.md class = %s, want document", mdClass)
	}
}

func TestWalkerIdempotent(t *testing.T) {
	walkRoot := t.TempDir()
	dbDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(walkRoot, "f.txt"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dbDir, "w.db")
	db, err := store.Open(store.Options{Path: dbPath, Tier: store.TierMid})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := store.Migrate(db, ""); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	ctx := context.Background()
	if _, err := Walk(ctx, db, Options{Root: walkRoot}); err != nil {
		t.Fatal(err)
	}
	// Run again — should upsert, not error
	if _, err := Walk(ctx, db, Options{Root: walkRoot}); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM files`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("file count = %d after 2 walks, want 1 (upsert)", count)
	}
}
