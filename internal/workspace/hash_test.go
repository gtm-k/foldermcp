package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHashFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	content := []byte("hello world\n")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	hash1, err := HashFile(path)
	if err != nil {
		t.Fatalf("HashFile failed: %v", err)
	}

	// Verify format is "sha256:<hex>".
	if !strings.HasPrefix(hash1, "sha256:") {
		t.Errorf("hash should start with 'sha256:', got %q", hash1)
	}

	// Hash should be deterministic.
	hash2, err := HashFile(path)
	if err != nil {
		t.Fatalf("second HashFile failed: %v", err)
	}
	if hash1 != hash2 {
		t.Errorf("hash not deterministic: %q vs %q", hash1, hash2)
	}

	// Different content should produce different hash.
	path2 := filepath.Join(dir, "test2.txt")
	if err := os.WriteFile(path2, []byte("different content\n"), 0644); err != nil {
		t.Fatalf("write second test file: %v", err)
	}
	hash3, err := HashFile(path2)
	if err != nil {
		t.Fatalf("HashFile for different file failed: %v", err)
	}
	if hash1 == hash3 {
		t.Errorf("different files produced same hash: %q", hash1)
	}
}

func TestHashFile_NotFound(t *testing.T) {
	_, err := HashFile("/nonexistent/file.txt")
	if err == nil {
		t.Error("expected error for non-existent file, got nil")
	}
}

func TestVerifyHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "verify.txt")

	content := []byte("verify me\n")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	// Get the correct hash.
	correctHash, err := HashFile(path)
	if err != nil {
		t.Fatalf("HashFile failed: %v", err)
	}

	// Correct hash should verify.
	ok, err := VerifyHash(path, correctHash)
	if err != nil {
		t.Fatalf("VerifyHash failed: %v", err)
	}
	if !ok {
		t.Error("VerifyHash returned false for correct hash")
	}

	// Wrong hash should not verify.
	ok, err = VerifyHash(path, "sha256:0000000000000000000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Fatalf("VerifyHash with wrong hash failed: %v", err)
	}
	if ok {
		t.Error("VerifyHash returned true for wrong hash")
	}
}

func TestVerifyHash_NotFound(t *testing.T) {
	_, err := VerifyHash("/nonexistent/file.txt", "sha256:abc")
	if err == nil {
		t.Error("expected error for non-existent file, got nil")
	}
}
