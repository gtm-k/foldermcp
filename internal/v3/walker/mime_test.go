//go:build cgo

package walker

import (
	"os"
	"path/filepath"
	"strings"
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

// TestClassifyFileBinaryWithTextExtension (Q2 Gap A, route 1 — extension
// precedence): a binary blob saved with a text-document extension must be
// reclassified non-indexable, not chunked as prose. The extension switch would
// otherwise label it 'document'; the content-based NUL check overrides that.
func TestClassifyFileBinaryWithTextExtension(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "renamed.txt")
	if err := os.WriteFile(path, []byte("hi\x00\x01\x02 binary masquerading as text"), 0644); err != nil {
		t.Fatal(err)
	}
	_, class, err := ClassifyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if class == "code" || class == "document" {
		t.Errorf("class = %q, want a non-indexable class (binary content must not be prose-chunked)", class)
	}
}

// TestClassifyFileBinaryBodyPastSniffWindow (Q2 Gap A, route 2 — 512-byte
// blind spot): a file whose first 512 bytes are clean ASCII (so DetectContentType
// returns text/plain) but whose body then turns binary must still be caught,
// because ClassifyFile samples well past the 512-byte sniff window.
func TestClassifyFileBinaryBodyPastSniffWindow(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "header-then-binary.log")
	body := append([]byte(strings.Repeat("a", 600)), 0x00, 0x01, 0x02)
	if err := os.WriteFile(path, body, 0644); err != nil {
		t.Fatal(err)
	}
	_, class, err := ClassifyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if class == "code" || class == "document" {
		t.Errorf("class = %q, want a non-indexable class (binary tail past byte 512 must be detected)", class)
	}
}

// TestClassifyFileUTF16Skipped (Q2 — lossy direction): UTF-16 text is
// NUL-interleaved; M1 has no transcoder, so indexing its raw bytes yields
// mojibake. It must be classed non-indexable (honest skip) until M2 adds
// UTF-8 transcoding.
func TestClassifyFileUTF16Skipped(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "windows-note.txt")
	// UTF-16LE BOM + "hello" (ASCII interleaved with NUL high bytes).
	utf16 := []byte{0xff, 0xfe, 'h', 0x00, 'e', 0x00, 'l', 0x00, 'l', 0x00, 'o', 0x00}
	if err := os.WriteFile(path, utf16, 0644); err != nil {
		t.Fatal(err)
	}
	_, class, err := ClassifyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if class == "code" || class == "document" {
		t.Errorf("class = %q, want non-indexable (UTF-16 has no M1 text path)", class)
	}
}

// TestClassifyFileBinaryDocumentExempt (Q2 review fix): a real PDF/DOCX/ODT
// carries NUL bytes in its first 8 KB, but it must keep content_class 'document'
// (NOT be downgraded to 'unknown' by the content-binary override) so the
// pipeline's dedicated binary-document path tags it 'binary_document_pending_m2'
// and the future M2 extractor still sees it. Without the exemption the override
// would silently route every real PDF/Office doc around that path.
func TestClassifyFileBinaryDocumentExempt(t *testing.T) {
	tmp := t.TempDir()
	for _, name := range []string{"report.pdf", "memo.docx", "sheet.odt"} {
		path := filepath.Join(tmp, name)
		// NUL in the first bytes, like a real compressed PDF / ZIP-based doc.
		if err := os.WriteFile(path, []byte("%PDF-1.7\n\x00\x01\x02 compressed stream bytes"), 0644); err != nil {
			t.Fatal(err)
		}
		_, class, err := ClassifyFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if class != "document" {
			t.Errorf("%s class = %q, want document (binary documents are exempt from the NUL override)", name, class)
		}
	}
}

// TestClassifyFileStructuredDataRouted (A5 / R3 reroute): csv/tsv/json/yaml/yml/
// xml files keep content_class 'data' (no new class value — the files
// content_class CHECK is not widened, A5 adds no migration) but ARE recognized
// by IsStructuredDataExt so the runner routes them to the csv/data chunker
// instead of skipping. Clean (non-binary) data files must keep class 'data'
// (NOT be downgraded to 'unknown' by the override).
func TestClassifyFileStructuredDataRouted(t *testing.T) {
	tmp := t.TempDir()
	cases := []struct {
		name, body string
	}{
		{"table.csv", "id,name\n1,alice\n2,bob\n"},
		{"table.tsv", "id\tname\n1\talice\n"},
		{"config.json", `{"a":1,"b":"x"}`},
		{"config.yaml", "a: 1\nb: x\n"},
		{"config.yml", "a: 1\n"},
		{"doc.xml", "<r><a>1</a></r>"},
	}
	for _, c := range cases {
		path := filepath.Join(tmp, c.name)
		if err := os.WriteFile(path, []byte(c.body), 0644); err != nil {
			t.Fatal(err)
		}
		_, class, err := ClassifyFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if class != "data" {
			t.Errorf("%s class = %q, want data (clean structured data keeps 'data')", c.name, class)
		}
		if !IsStructuredDataExt(path) {
			t.Errorf("%s: IsStructuredDataExt=false, want true (runner must route it)", c.name)
		}
	}
}

// TestClassifyFileBinaryJSONStillSkipped (A5 / R3 BLOCKING): a .json whose
// content is actually binary (NUL bytes) must NOT be indexed by extension — the
// IsBinaryContent NUL gate downgrades it to 'unknown' so the runner skips it.
// Index by CONTENT, never blindly by extension.
func TestClassifyFileBinaryJSONStillSkipped(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "actually-binary.json")
	if err := os.WriteFile(path, []byte("{\x00\x01\x02 not really json}"), 0644); err != nil {
		t.Fatal(err)
	}
	_, class, err := ClassifyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if class != "unknown" {
		t.Errorf("class = %q, want unknown (NUL-bearing .json must be downgraded + skipped, R3)", class)
	}
}

// TestIsStructuredDataExtExcludesSqlite: .sqlite is a binary DB container and
// must NOT be treated as a structure-aware text format.
func TestIsStructuredDataExtExcludesSqlite(t *testing.T) {
	if IsStructuredDataExt("/tmp/store.sqlite") {
		t.Error("IsStructuredDataExt(.sqlite)=true, want false (binary DB, not text)")
	}
	if IsStructuredDataExt("/tmp/main.go") {
		t.Error("IsStructuredDataExt(.go)=true, want false")
	}
}

// TestClassifyFileCleanTextStillIndexable (regression): a normal text/code file
// with no NUL bytes must keep its indexable classification — the binary override
// must not over-trigger.
func TestClassifyFileCleanTextStillIndexable(t *testing.T) {
	tmp := t.TempDir()
	bigDoc := filepath.Join(tmp, "manual.txt")
	// Larger than the 512-byte sniff window, all clean UTF-8 text.
	if err := os.WriteFile(bigDoc, []byte(strings.Repeat("the quick brown fox. ", 500)), 0644); err != nil {
		t.Fatal(err)
	}
	_, class, err := ClassifyFile(bigDoc)
	if err != nil {
		t.Fatal(err)
	}
	if class != "document" {
		t.Errorf("class = %q, want document (clean text must stay indexable)", class)
	}
}
