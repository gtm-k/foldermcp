//go:build cgo && !windows

package grpc

import (
	"os"
	"testing"
)

func TestInitWritesMode0600(t *testing.T) {
	tmp := t.TempDir()
	paths := DefaultAuthPaths(tmp)

	if err := Init(paths); err != nil {
		t.Fatalf("Init: %v", err)
	}

	checks := []string{paths.TokenFile, paths.CertFile, paths.KeyFile}
	for _, p := range checks {
		info, err := os.Stat(p)
		if err != nil {
			t.Errorf("%s: %v", p, err)
			continue
		}
		if info.Mode().Perm() != 0600 {
			t.Errorf("%s mode = %o, want 0600", p, info.Mode().Perm())
		}
	}

	dirInfo, err := os.Stat(paths.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0700 {
		t.Errorf("dir mode = %o, want 0700", dirInfo.Mode().Perm())
	}
}

func TestInitIsIdempotent(t *testing.T) {
	tmp := t.TempDir()
	paths := DefaultAuthPaths(tmp)

	if err := Init(paths); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(paths.TokenFile)

	// Second call should be a no-op
	if err := Init(paths); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(paths.TokenFile)

	if string(before) != string(after) {
		t.Error("Init changed token on second call (should be idempotent)")
	}
}

func TestRotateChangesToken(t *testing.T) {
	tmp := t.TempDir()
	paths := DefaultAuthPaths(tmp)
	if err := Init(paths); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(paths.TokenFile)
	if err := Rotate(paths); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(paths.TokenFile)
	if string(before) == string(after) {
		t.Error("token did not change after rotate")
	}
}
