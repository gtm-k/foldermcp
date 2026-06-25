//go:build cgo

package chunker

import (
	"errors"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// pdfFixture returns the absolute path to a committed PDF test fixture under
// testdata/v3/pdf (three levels up from internal/v3/chunker).
func pdfFixture(name string) string {
	return filepath.Join("..", "..", "..", "testdata", "v3", "pdf", name)
}

// requirePdftotext skips the test when the binary is absent so a dev box without
// poppler can still run the rest of the suite. The WSL gate has it installed.
func requirePdftotext(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext not on PATH; skipping (gate runs in WSL where it is installed)")
	}
}

var headerRe = regexp.MustCompile(`^p\d+¶\d+`)

// TestChunkPDF_GoldenHeadersAndSpans is the model-free golden test (Phase 4 AC):
// every chunk header matches ^p\d+¶\d+, and re-slicing the EXTRACTED text by
// [byte_start:byte_end] reproduces the chunk text exactly.
func TestChunkPDF_GoldenHeadersAndSpans(t *testing.T) {
	requirePdftotext(t)

	// Independently extract the text so the test can re-slice by byte offsets.
	extracted, err := extractPDFText(pdfFixture("multi_page.pdf"), PDFConfig{})
	if err != nil {
		t.Fatalf("extractPDFText: %v", err)
	}

	chunks, stats, err := ChunkPDF(pdfFixture("multi_page.pdf"), "multi_page.pdf", DefaultConfig(), wordCounter{}, PDFConfig{})
	if err != nil {
		t.Fatalf("ChunkPDF: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("ChunkPDF produced zero chunks")
	}
	if stats.Pages != 2 {
		t.Errorf("stats.Pages = %d, want 2", stats.Pages)
	}
	if stats.ExtractorVersion != pdfExtractorVersion {
		t.Errorf("ExtractorVersion = %q, want %q", stats.ExtractorVersion, pdfExtractorVersion)
	}

	for i, c := range chunks {
		if !headerRe.MatchString(c.Header) {
			t.Errorf("chunk %d header %q does not match ^p\\d+¶\\d+", i, c.Header)
		}
		if c.ByteStart < 0 || c.ByteEnd > len(extracted) || c.ByteStart > c.ByteEnd {
			t.Fatalf("chunk %d span [%d:%d] out of range (extracted len %d)", i, c.ByteStart, c.ByteEnd, len(extracted))
		}
		if got := extracted[c.ByteStart:c.ByteEnd]; got != c.Text {
			t.Errorf("chunk %d re-slice mismatch:\n got=%q\nwant=%q", i, got, c.Text)
		}
	}
}

// TestChunkPDF_NoChunkCrossesFormFeed asserts that no chunk's byte range spans a
// form-feed (page) boundary — each page is chunked over its own slice.
func TestChunkPDF_NoChunkCrossesFormFeed(t *testing.T) {
	requirePdftotext(t)

	extracted, err := extractPDFText(pdfFixture("multi_page.pdf"), PDFConfig{})
	if err != nil {
		t.Fatalf("extractPDFText: %v", err)
	}
	// Byte positions of every form-feed in the extracted text.
	var ffPositions []int
	for i := 0; i < len(extracted); i++ {
		if extracted[i] == '\f' {
			ffPositions = append(ffPositions, i)
		}
	}
	if len(ffPositions) == 0 {
		t.Fatal("fixture has no form-feed; cannot exercise the boundary invariant")
	}

	chunks, _, err := ChunkPDF(pdfFixture("multi_page.pdf"), "multi_page.pdf", DefaultConfig(), wordCounter{}, PDFConfig{})
	if err != nil {
		t.Fatalf("ChunkPDF: %v", err)
	}
	for i, c := range chunks {
		for _, ff := range ffPositions {
			if c.ByteStart <= ff && ff < c.ByteEnd {
				t.Errorf("chunk %d span [%d:%d] crosses form-feed at %d", i, c.ByteStart, c.ByteEnd, ff)
			}
		}
		if strings.ContainsRune(c.Text, '\f') {
			t.Errorf("chunk %d text contains a form-feed", i)
		}
	}
}

// TestChunkPDF_ExtractionQuality: a near-empty/image-only PDF (median <200
// chars/page) returns ErrExtractionQuality so the runner marks it failed
// visibly rather than indexing an empty document.
func TestChunkPDF_ExtractionQuality(t *testing.T) {
	requirePdftotext(t)

	_, stats, err := ChunkPDF(pdfFixture("scanned_like.pdf"), "scanned_like.pdf", DefaultConfig(), wordCounter{}, PDFConfig{})
	if !errors.Is(err, ErrExtractionQuality) {
		t.Fatalf("err = %v, want ErrExtractionQuality (stats=%+v)", err, stats)
	}
}

// TestChunkPDF_MissingBinary: an unresolvable pdftotext path yields
// ErrDependencyMissing (mapped by the runner to error_message='dependency:pdftotext').
func TestChunkPDF_MissingBinary(t *testing.T) {
	_, _, err := ChunkPDF(pdfFixture("multi_page.pdf"), "multi_page.pdf", DefaultConfig(), wordCounter{},
		PDFConfig{PdftotextPath: filepath.Join(t.TempDir(), "no_such_pdftotext")})
	if !errors.Is(err, ErrDependencyMissing) {
		t.Fatalf("err = %v, want ErrDependencyMissing", err)
	}
}

// TestPdftotextAvailable reports true when the binary resolves and false for a
// bogus override (the runner uses this for its single startup WARN).
func TestPdftotextAvailable(t *testing.T) {
	if got := PdftotextAvailable(PDFConfig{PdftotextPath: filepath.Join(t.TempDir(), "nope")}); got {
		t.Error("PdftotextAvailable = true for a nonexistent override")
	}
}
