//go:build cgo

package walker

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
)

func TestHashFileMatchesDirectHash(t *testing.T) {
	tmp := t.TempDir()
	content := []byte("hello world — test content for hashing")
	path := filepath.Join(tmp, "test.txt")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}

	got, err := HashFile(path)
	if err != nil {
		t.Fatal(err)
	}

	want := sha256.Sum256(content)
	if len(got) != sha256.Size {
		t.Fatalf("hash len = %d, want %d", len(got), sha256.Size)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("hash mismatch at byte %d", i)
		}
	}
}
