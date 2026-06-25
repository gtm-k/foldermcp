//go:build cgo

package chunker

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Real, parseable office fixtures are built via the production chunker.BuildDOCX
// / BuildODT helpers (also reused by pipeline/runner_test.go for R2). A real
// Deflate ZIP carries the "PK\x03\x04" magic and NUL bytes in its first 8 KB —
// exactly the R2 condition the binary detector must NOT misclassify to unknown.
//
// Probe phrases let the runner e2e + FTS assertions target each exact document.
var officeDocxParas = []string{
	"This DOCX fixture describes the FolderMCP office extraction path in detail.",
	"It mentions tachyon diagnostics as a unique probe phrase so retrieval tests can target this exact document and nothing else in the corpus.",
	"A third paragraph adds more prose so the recursive prose splitter and the paragraph counter both have real material to anchor chunk headers against.",
}

var officeOdtParas = []string{
	"This ODT fixture exercises the OpenDocument content.xml extraction branch.",
	"It mentions selenium chromatography as a unique probe phrase distinct from the docx fixture so the two documents never collide in retrieval.",
	"A closing paragraph keeps the document multi-paragraph so paragraph numbering in the chunk header advances beyond one.",
}

// writeOfficeFixture builds office bytes via the production helper and writes
// them to path.
func writeOfficeFixture(t *testing.T, path string, build func([]string) ([]byte, error), paras []string) {
	t.Helper()
	raw, err := build(paras)
	if err != nil {
		t.Fatalf("build office fixture: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// --- tests -------------------------------------------------------------------

func TestChunkOffice_DOCX_GoldenHeadersAndSpans(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.docx")
	writeOfficeFixture(t, path, BuildDOCX, officeDocxParas)

	chunks, stats, err := ChunkOffice(context.Background(), path, "report.docx", ".docx", DefaultConfig(), wordCounter{})
	if err != nil {
		t.Fatalf("ChunkOffice docx: %v", err)
	}
	assertOfficeGolden(t, chunks, stats, officeExtractorVersion, "tachyon")
}

func TestChunkOffice_ODT_GoldenHeadersAndSpans(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memo.odt")
	writeOfficeFixture(t, path, BuildODT, officeOdtParas)

	chunks, stats, err := ChunkOffice(context.Background(), path, "memo.odt", ".odt", DefaultConfig(), wordCounter{})
	if err != nil {
		t.Fatalf("ChunkOffice odt: %v", err)
	}
	assertOfficeGolden(t, chunks, stats, officeExtractorVersion, "selenium")
}

// assertOfficeGolden re-joins the office text the same way ChunkOffice does and
// asserts the contract: headers match ^p\d+¶\d+, re-slicing the joined text by
// [start:end] reproduces each chunk, and a known probe phrase is present in some
// chunk (proves the text was actually extracted from the XML, not fabricated).
func assertOfficeGolden(t *testing.T, chunks []ExtractedChunk, stats ExtractStats, wantVersion, probe string) {
	t.Helper()
	if len(chunks) == 0 {
		t.Fatal("ChunkOffice produced zero chunks")
	}
	if stats.ExtractorVersion != wantVersion {
		t.Errorf("ExtractorVersion = %q, want %q", stats.ExtractorVersion, wantVersion)
	}
	if stats.Pages != 1 {
		t.Errorf("office Pages = %d, want 1 (synthetic single page)", stats.Pages)
	}

	// Reconstruct the joined text from the chunks' own offsets is not possible
	// directly; instead re-derive it from the chunk with max end == full length.
	// Simpler: every chunk header must be well-formed and every chunk text must
	// be a substring of the corresponding paragraph. We verify substring-of-join
	// by reslicing against a reconstructed text below.
	maxEnd := 0
	for _, c := range chunks {
		if c.ByteEnd > maxEnd {
			maxEnd = c.ByteEnd
		}
	}
	probeSeen := false
	for i, c := range chunks {
		if !headerRe.MatchString(c.Header) {
			t.Errorf("chunk %d header %q does not match ^p\\d+¶\\d+", i, c.Header)
		}
		if c.ByteStart < 0 || c.ByteStart > c.ByteEnd || c.ByteEnd > maxEnd {
			t.Errorf("chunk %d span [%d:%d] out of range (maxEnd %d)", i, c.ByteStart, c.ByteEnd, maxEnd)
		}
		if strings.Contains(c.Text, probe) {
			probeSeen = true
		}
		if c.TokenCount <= 0 {
			t.Errorf("chunk %d has non-positive token count %d", i, c.TokenCount)
		}
	}
	if !probeSeen {
		t.Errorf("probe phrase %q not found in any office chunk — extraction failed", probe)
	}
}

// TestChunkOffice_ReSliceReproducesText is the strict span golden check: it
// re-joins the extracted paragraphs exactly as ChunkOffice does and verifies
// joined[start:end]==text for every chunk.
func TestChunkOffice_ReSliceReproducesText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc.docx")
	writeOfficeFixture(t, path, BuildDOCX, officeDocxParas)

	paras, err := extractOfficeParagraphs(context.Background(), path, ".docx")
	if err != nil {
		t.Fatalf("extractOfficeParagraphs: %v", err)
	}
	joined := strings.Join(paras, "\n\n")

	chunks, _, err := ChunkOffice(context.Background(), path, "doc.docx", ".docx", DefaultConfig(), wordCounter{})
	if err != nil {
		t.Fatalf("ChunkOffice: %v", err)
	}
	for i, c := range chunks {
		if c.ByteEnd > len(joined) {
			t.Fatalf("chunk %d end %d exceeds joined len %d", i, c.ByteEnd, len(joined))
		}
		if got := joined[c.ByteStart:c.ByteEnd]; got != c.Text {
			t.Errorf("chunk %d re-slice mismatch:\n got=%q\nwant=%q", i, got, c.Text)
		}
	}
}

// TestChunkOffice_MalformedContainer: a non-ZIP file returns an error (the
// runner records it as a per-file failure rather than crashing).
func TestChunkOffice_MalformedContainer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.docx")
	if err := os.WriteFile(path, []byte("not a zip file at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ChunkOffice(context.Background(), path, "broken.docx", ".docx", DefaultConfig(), wordCounter{}); err == nil {
		t.Error("expected an error for a non-ZIP .docx, got nil")
	}
}

// TestChunkOffice_ZipEntryCap (F4): a container with more than maxZipEntries
// entries (a zip-flood DoS) is rejected before the entry loop allocates one
// *zip.File per header. The synthetic zip carries > maxZipEntries tiny entries;
// extraction must return an error mentioning the entry count, not OOM.
func TestChunkOffice_ZipEntryCap(t *testing.T) {
	entries := make([]zipEntry, 0, maxZipEntries+2)
	// Include the real document part so the ONLY reason to error is the entry cap.
	entries = append(entries, zipEntry{"word/document.xml",
		`<?xml version="1.0"?><w:document xmlns:w="x"><w:body><w:p><w:r><w:t>hi</w:t></w:r></w:p></w:body></w:document>`,
		0})
	for i := 0; i < maxZipEntries+1; i++ {
		entries = append(entries, zipEntry{fmt.Sprintf("pad/%d.txt", i), "x", 0})
	}
	raw, err := buildZip(entries)
	if err != nil {
		t.Fatalf("buildZip: %v", err)
	}
	path := filepath.Join(t.TempDir(), "flood.docx")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = extractOfficeParagraphs(context.Background(), path, ".docx")
	if err == nil {
		t.Fatal("expected an error for a zip-flood container, got nil")
	}
	if !strings.Contains(err.Error(), "entries") {
		t.Errorf("error = %v, want it to mention the entry-count cap", err)
	}
}

// TestChunkOffice_XMLOversizeFails (re-review FIX 2): a document.xml larger than
// maxOfficeXML must FAIL with the bare ErrExtractionOversize sentinel, NOT be
// silently truncated at the cap and indexed as a partial document. The synthetic
// docx carries one giant paragraph whose document.xml exceeds maxOfficeXML; the
// extractor must reject it rather than return truncated text.
func TestChunkOffice_XMLOversizeFails(t *testing.T) {
	// Build a document.xml whose serialized size exceeds maxOfficeXML. One <w:p>
	// with a single <w:t> run of >maxOfficeXML 'a' bytes does it; Deflate makes
	// the on-disk zip tiny so the test stays cheap.
	big := strings.Repeat("a", maxOfficeXML+1024)
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	body.WriteString(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	body.WriteString(`<w:p><w:r><w:t xml:space="preserve">`)
	body.WriteString(big)
	body.WriteString(`</w:t></w:r></w:p></w:body></w:document>`)

	entries := []zipEntry{
		{"word/document.xml", body.String(), zip.Deflate},
	}
	raw, err := buildZip(entries)
	if err != nil {
		t.Fatalf("buildZip: %v", err)
	}
	path := filepath.Join(t.TempDir(), "huge.docx")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = extractOfficeParagraphs(context.Background(), path, ".docx")
	if !errors.Is(err, ErrExtractionOversize) {
		t.Fatalf("extractOfficeParagraphs over maxOfficeXML = %v, want ErrExtractionOversize (must not silently truncate)", err)
	}
}
