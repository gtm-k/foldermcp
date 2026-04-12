//go:build cgo && !windows

package grpc

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListenUnixSocketMode0600(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "sock")

	l, err := ListenUnixSocket(p)
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

func TestListenUnixSocketCreatesDir(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "nested", "dir", "sock")

	l, err := ListenUnixSocket(p)
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
