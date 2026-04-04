package introspect

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResourceIntrospector_DiscoverFiles(t *testing.T) {
	// Create a temp directory with various file types.
	dir := t.TempDir()

	// Create resource files.
	writeFile(t, dir, "guide.pdf", "fake PDF content")
	writeFile(t, dir, "notes.md", "# Notes\nSome markdown content")
	writeFile(t, dir, "photo.png", "fake PNG content")
	writeFile(t, dir, "data.csv", "col1,col2\na,b")

	// Create code files that should be skipped.
	writeFile(t, dir, "calculator.py", "def calc(): pass")
	writeFile(t, dir, "app.ts", "export function main() {}")
	writeFile(t, dir, "util.js", "module.exports = {}")
	writeFile(t, dir, "setup.sh", "#!/bin/bash")

	// Create a file with unknown extension that should be skipped.
	writeFile(t, dir, "binary.exe", "MZ...")

	ri := &ResourceIntrospector{}
	includes := []string{"**/*.pdf", "**/*.md", "**/*.png", "**/*.csv"}
	excludes := []string{}

	resources, err := ri.Discover(dir, includes, excludes)
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	// Should find 4 resource files, not the code files or unknown files.
	if len(resources) != 4 {
		t.Fatalf("expected 4 resources, got %d", len(resources))
	}

	// Build a map of discovered names.
	found := make(map[string]ResourceMetadata)
	for _, r := range resources {
		found[r.Name] = r
	}

	// Verify each expected resource was found.
	for _, name := range []string{"guide.pdf", "notes.md", "photo.png", "data.csv"} {
		if _, ok := found[name]; !ok {
			t.Errorf("expected resource %q not found", name)
		}
	}

	// Verify code files were NOT discovered.
	for _, name := range []string{"calculator.py", "app.ts", "util.js", "setup.sh", "binary.exe"} {
		if _, ok := found[name]; ok {
			t.Errorf("code/unknown file %q should not be discovered as a resource", name)
		}
	}
}

func TestResourceIntrospector_SkipsLargeFiles(t *testing.T) {
	dir := t.TempDir()

	// Create a file that exceeds 50 MB.
	largePath := filepath.Join(dir, "huge.csv")
	f, err := os.Create(largePath)
	if err != nil {
		t.Fatalf("create large file: %v", err)
	}

	// Write just enough to exceed the limit (50 MB + 1 byte).
	// Use Truncate to quickly set the size without writing data.
	if err := f.Truncate(50*1024*1024 + 1); err != nil {
		_ = f.Close()
		t.Fatalf("truncate: %v", err)
	}
	_ = f.Close()

	// Also create a small file to verify normal discovery still works.
	writeFile(t, dir, "small.csv", "a,b,c")

	ri := &ResourceIntrospector{}
	resources, err := ri.Discover(dir, []string{"**/*.csv"}, nil)
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	// Should only find the small file, not the huge one.
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	if resources[0].Name != "small.csv" {
		t.Errorf("expected small.csv, got %q", resources[0].Name)
	}
}

func TestResourceIntrospector_MimeDetection(t *testing.T) {
	dir := t.TempDir()

	testCases := []struct {
		filename string
		wantMime string
		wantType string
	}{
		{"doc.pdf", "application/pdf", "document"},
		{"readme.md", "text/markdown", "document"},
		{"notes.txt", "text/plain", "document"},
		{"photo.png", "image/png", "image"},
		{"photo.jpg", "image/jpeg", "image"},
		{"photo.jpeg", "image/jpeg", "image"},
		{"diagram.svg", "image/svg+xml", "image"},
		{"anim.gif", "image/gif", "image"},
		{"data.csv", "text/csv", "data"},
		{"config.json", "application/json", "data"},
		{"config.yaml", "text/yaml", "config"},
		{"config.yml", "text/yaml", "config"},
		{"config.toml", "text/toml", "config"},
		// .env files are skipped for security (SEC-02).
		{"sheet.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "data"},
		{"report.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "document"},
	}

	// Use a broad include pattern.
	includes := []string{
		"**/*.pdf", "**/*.md", "**/*.txt", "**/*.png", "**/*.jpg", "**/*.jpeg",
		"**/*.svg", "**/*.gif", "**/*.csv", "**/*.json", "**/*.yaml", "**/*.yml",
		"**/*.toml", "**/*.xlsx", "**/*.docx",
	}

	for _, tc := range testCases {
		writeFile(t, dir, tc.filename, "test content")
	}

	ri := &ResourceIntrospector{}
	resources, err := ri.Discover(dir, includes, nil)
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	found := make(map[string]ResourceMetadata)
	for _, r := range resources {
		found[r.Name] = r
	}

	for _, tc := range testCases {
		r, ok := found[tc.filename]
		if !ok {
			t.Errorf("resource %q not found", tc.filename)
			continue
		}
		if r.MimeType != tc.wantMime {
			t.Errorf("%s: MimeType = %q, want %q", tc.filename, r.MimeType, tc.wantMime)
		}
		if r.Type != tc.wantType {
			t.Errorf("%s: Type = %q, want %q", tc.filename, r.Type, tc.wantType)
		}
	}
}

func TestResourceIntrospector_RespectsExcludes(t *testing.T) {
	dir := t.TempDir()

	// Create a docs directory and a node_modules directory.
	docsDir := filepath.Join(dir, "docs")
	nmDir := filepath.Join(dir, "node_modules", "pkg")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(nmDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, docsDir, "api.md", "# API")
	writeFile(t, nmDir, "readme.md", "# Package")
	writeFile(t, dir, "README.md", "# Project")

	ri := &ResourceIntrospector{}
	resources, err := ri.Discover(dir,
		[]string{"**/*.md"},
		[]string{"node_modules/**", "README.md"},
	)
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	// Should only find docs/api.md — node_modules and README.md are excluded.
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	if resources[0].Name != "api.md" {
		t.Errorf("expected api.md, got %q", resources[0].Name)
	}
}

func TestResourceIntrospector_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()

	ri := &ResourceIntrospector{}
	resources, err := ri.Discover(dir, []string{"**/*.md"}, nil)
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}
	if len(resources) != 0 {
		t.Errorf("expected 0 resources, got %d", len(resources))
	}
}

// writeFile creates a file in dir with the given name and content.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
