//go:build cgo

package walker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClassifyFilePython(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "main.py")
	if err := os.WriteFile(path, []byte("print('hi')"), 0644); err != nil {
		t.Fatal(err)
	}
	_, class, err := ClassifyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if class != "code" {
		t.Errorf("class = %q, want code", class)
	}
}

func TestClassifyFileMarkdown(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "README.md")
	if err := os.WriteFile(path, []byte("# Hello"), 0644); err != nil {
		t.Fatal(err)
	}
	_, class, err := ClassifyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if class != "document" {
		t.Errorf("class = %q, want document", class)
	}
}

func TestClassifyFilePNG(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "image.png")
	// Write PNG magic bytes
	if err := os.WriteFile(path, []byte("\x89PNG\r\n\x1a\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, class, err := ClassifyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if class != "image" {
		t.Errorf("class = %q, want image", class)
	}
}
