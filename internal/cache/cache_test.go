package cache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCache_GetCopiesFile(t *testing.T) {
	srcDir := t.TempDir()
	cacheDir := t.TempDir()

	srcFile := filepath.Join(srcDir, "tool.py")
	if err := os.WriteFile(srcFile, []byte("print('hello')"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := New(cacheDir)
	localPath, err := c.Get(srcFile)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}

	// Verify the cached file exists and has the same content.
	data, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("read cached file: %v", err)
	}
	if string(data) != "print('hello')" {
		t.Errorf("cached content = %q, want %q", string(data), "print('hello')")
	}

	// Verify it's in the cache directory, not the source directory.
	if filepath.Dir(filepath.Dir(localPath)) != cacheDir {
		t.Errorf("cached file %q is not under cache dir %q", localPath, cacheDir)
	}
}

func TestCache_GetReturnsCached(t *testing.T) {
	srcDir := t.TempDir()
	cacheDir := t.TempDir()

	srcFile := filepath.Join(srcDir, "tool.py")
	if err := os.WriteFile(srcFile, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := New(cacheDir)

	// First call: copies file.
	path1, err := c.Get(srcFile)
	if err != nil {
		t.Fatalf("first Get() error: %v", err)
	}

	// Second call with unchanged source: should return same cached path.
	path2, err := c.Get(srcFile)
	if err != nil {
		t.Fatalf("second Get() error: %v", err)
	}

	if path1 != path2 {
		t.Errorf("expected same cached path, got %q and %q", path1, path2)
	}

	// Verify content is still correct.
	data, err := os.ReadFile(path2)
	if err != nil {
		t.Fatalf("read cached file: %v", err)
	}
	if string(data) != "v1" {
		t.Errorf("cached content = %q, want %q", string(data), "v1")
	}
}

func TestCache_InvalidateForceReread(t *testing.T) {
	srcDir := t.TempDir()
	cacheDir := t.TempDir()

	srcFile := filepath.Join(srcDir, "tool.py")
	if err := os.WriteFile(srcFile, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := New(cacheDir)

	// Cache the file.
	cachedPath, err := c.Get(srcFile)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}

	// Verify initial content.
	data, err := os.ReadFile(cachedPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "original" {
		t.Fatalf("initial content = %q, want %q", string(data), "original")
	}

	// Update the source file.
	if err := os.WriteFile(srcFile, []byte("updated"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Invalidate cached copy.
	if err := c.Invalidate(srcFile); err != nil {
		t.Fatalf("Invalidate() error: %v", err)
	}

	// Verify cached file was removed.
	if _, err := os.Stat(cachedPath); !os.IsNotExist(err) {
		t.Error("expected cached file to be removed after Invalidate")
	}

	// Get again: should re-copy updated content.
	newPath, err := c.Get(srcFile)
	if err != nil {
		t.Fatalf("Get() after Invalidate error: %v", err)
	}

	data, err = os.ReadFile(newPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "updated" {
		t.Errorf("content after invalidate = %q, want %q", string(data), "updated")
	}
}

func TestCache_GetUpdatesStaleCache(t *testing.T) {
	srcDir := t.TempDir()
	cacheDir := t.TempDir()

	srcFile := filepath.Join(srcDir, "tool.py")
	if err := os.WriteFile(srcFile, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := New(cacheDir)

	// Cache it.
	_, err := c.Get(srcFile)
	if err != nil {
		t.Fatal(err)
	}

	// Modify source without invalidation.
	if err := os.WriteFile(srcFile, []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Get should detect the hash mismatch and re-copy.
	path, err := c.Get(srcFile)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "v2" {
		t.Errorf("content = %q, want %q", string(data), "v2")
	}
}
