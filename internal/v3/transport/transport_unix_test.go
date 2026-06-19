//go:build !windows

package transport

import (
	"os"
	"path/filepath"
	"testing"
)

// TestListenSocketMode0600 verifies the Unix socket is created owner-only.
func TestListenSocketMode0600(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sock")
	l, err := Listen(p)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = l.Close() }()

	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("socket mode = %o, want 0600", info.Mode().Perm())
	}
}

// TestListenCreatesDir verifies the socket's parent directory is created 0700.
func TestListenCreatesDir(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nested", "dir", "sock")
	l, err := Listen(p)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = l.Close() }()

	dirInfo, err := os.Stat(filepath.Dir(p))
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0700 {
		t.Errorf("dir mode = %o, want 0700", dirInfo.Mode().Perm())
	}
}
