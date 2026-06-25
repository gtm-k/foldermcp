//go:build cgo

package chunker

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// ExtractedChunk is a chunk produced by a binary-document extractor (PDF via
// pdftotext, Office via archive/zip). Byte offsets are RELATIVE to the full
// extracted text the extractor produced (D18), NOT to the original file bytes —
// the raw .pdf/.docx bytes are a compressed container, so a file-relative span
// would be meaningless. Re-slicing the extracted text by [ByteStart:ByteEnd]
// reproduces Text exactly (asserted by the golden tests).
type ExtractedChunk struct {
	Text       string
	ByteStart  int
	ByteEnd    int
	TokenCount int
	Header     string // "p<page>¶<para> — <doc title>" (FTS-indexed via chunks_fts(header))
}

// ExtractStats are per-file extraction statistics persisted in the file node's
// properties JSON (D18) so retrieval quality is observable per document.
type ExtractStats struct {
	Pages              int    `json:"pages"`
	CharsPerPageMedian int    `json:"chars_per_page_median"`
	ExtractorVersion   string `json:"extractor_version"`
}

// Sentinel errors so the runner can map a failure to the visible
// pipeline_state.error_message marker (D8) instead of crashing.
var (
	// ErrExtractionQuality means the document parsed but yielded too little
	// text to be a digital-native document (median <minCharsPerPage chars/page)
	// — almost always a scanned/image-only PDF, which is out of M2 scope (OCR is
	// post-gate). Surfaced as error_message='extraction_quality'.
	ErrExtractionQuality = errors.New("extraction_quality")
	// ErrDependencyMissing means the external extractor binary (pdftotext) is
	// not on PATH. Surfaced as error_message='dependency:pdftotext'.
	ErrDependencyMissing = errors.New("dependency:pdftotext")
)

const (
	// pdfExtractorVersion tags the on-disk provenance of extracted text. Bump
	// when the extraction algorithm changes its output (R7: this is NOT in the
	// embedding fingerprint, so changing it needs an explicit reindex policy).
	pdfExtractorVersion = "pdftotext-layout-v1"
	// minCharsPerPage is the digital-native threshold (D18 / pre-mortem Story 1).
	// A median below this means the PDF is effectively image-only.
	minCharsPerPage = 200
	// formFeed is the page separator pdftotext emits between pages.
	formFeed = '\f'
)

// PDFConfig configures PDF extraction. PdftotextPath overrides binary discovery
// (config/env per D8); empty means look up "pdftotext" on PATH.
type PDFConfig struct {
	PdftotextPath string
}

// pdftotextBinary resolves the pdftotext binary, honoring an explicit override
// then falling back to PATH discovery (exec.LookPath, D8). Returns
// ErrDependencyMissing if neither resolves.
func pdftotextBinary(cfg PDFConfig) (string, error) {
	if cfg.PdftotextPath != "" {
		if _, err := os.Stat(cfg.PdftotextPath); err == nil {
			return cfg.PdftotextPath, nil
		}
		return "", fmt.Errorf("%w (configured path %q not found)", ErrDependencyMissing, cfg.PdftotextPath)
	}
	path, err := exec.LookPath("pdftotext")
	if err != nil {
		return "", ErrDependencyMissing
	}
	return path, nil
}

// PdftotextAvailable reports whether the pdftotext binary can be resolved. The
// runner calls this once at startup to emit a single WARN rather than one per
// file (D8).
func PdftotextAvailable(cfg PDFConfig) bool {
	_, err := pdftotextBinary(cfg)
	return err == nil
}

// extractPDFText runs `pdftotext -layout <path> -` once and returns the
// extracted text (form-feed page separators preserved). Zero network calls —
// it is a local subprocess. Returns ErrDependencyMissing if the binary is
// absent so the runner can mark the file failed visibly.
func extractPDFText(path string, cfg PDFConfig) (string, error) {
	bin, err := pdftotextBinary(cfg)
	if err != nil {
		return "", err
	}
	// -layout preserves the physical layout; "-" writes to stdout.
	cmd := exec.Command(bin, "-layout", path, "-")
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("pdftotext %s: %w (%s)", path, err, strings.TrimSpace(stderr.String()))
	}
	return out.String(), nil
}

// ChunkPDF extracts text from a PDF and produces page-anchored prose chunks.
// title is used in the chunk header (D18); pass the document base name.
//
// Algorithm (D8/D18): pdftotext -layout once → split pages on form-feed →
// paragraph-split per page → feed each page through the recursive prose splitter
// with page-anchored byte spans. Every chunk carries header "p<page>¶<para> —
// <title>" and byte offsets relative to the full extracted text. No chunk's byte
// range ever crosses a form-feed boundary (each page is chunked independently
// over its own slice of the extracted text).
func ChunkPDF(path, title string, cfg Config, counter Counter, pdfCfg PDFConfig) ([]ExtractedChunk, ExtractStats, error) {
	text, err := extractPDFText(path, pdfCfg)
	if err != nil {
		return nil, ExtractStats{}, err
	}
	return chunkExtractedPages(text, title, pdfExtractorVersion, cfg, counter)
}

// chunkExtractedPages is the shared page-anchored chunking core used by ChunkPDF
// (pages already separated by form-feed) and, after the office extractor
// synthesizes a single-page extraction, by ChunkOffice. It splits text on
// form-feed into pages, computes the digital-native quality gate, and chunks
// each page independently so no chunk spans a page boundary.
func chunkExtractedPages(text, title, extractorVersion string, cfg Config, counter Counter) ([]ExtractedChunk, ExtractStats, error) {
	if cfg == (Config{}) {
		cfg = DefaultConfig()
	}
	// Split on form-feed. pdftotext emits a trailing \f after the last page, so
	// the split can yield a trailing empty segment — keep page numbering aligned
	// by tracking byte offsets across the original text rather than re-joining.
	pages := splitPages(text)

	stats := ExtractStats{ExtractorVersion: extractorVersion}
	var charCounts []int
	var pageNum int
	var out []ExtractedChunk

	pageStart := 0
	for _, page := range pages {
		// Advance pageStart over the page text + its trailing form-feed (if any)
		// so byte offsets stay relative to the FULL extracted text.
		thisStart := pageStart
		pageStart += len(page.text) + page.sepLen

		trimmed := strings.TrimSpace(page.text)
		if trimmed == "" {
			continue // blank trailing segment from the final \f — not a real page
		}
		pageNum++
		charCounts = append(charCounts, len(trimmed))

		// Paragraph-split within the page, then recursively split each paragraph.
		paraNum := 0
		for _, para := range splitParagraphs(page.text) {
			if strings.TrimSpace(para.text) == "" {
				continue
			}
			paraNum++
			paraAbsStart := thisStart + para.offset
			for _, pc := range ChunkProse(para.text, cfg, counter) {
				out = append(out, ExtractedChunk{
					Text:       pc.Text,
					ByteStart:  paraAbsStart + pc.ByteStart,
					ByteEnd:    paraAbsStart + pc.ByteEnd,
					TokenCount: pc.TokenCount,
					Header:     fmt.Sprintf("p%d¶%d — %s", pageNum, paraNum, title),
				})
			}
		}
	}

	stats.Pages = pageNum
	stats.CharsPerPageMedian = median(charCounts)

	// Digital-native quality gate (D18): an image-only PDF extracts almost no
	// text; flag it visibly rather than indexing an empty document.
	if pageNum > 0 && stats.CharsPerPageMedian < minCharsPerPage {
		return nil, stats, ErrExtractionQuality
	}
	return out, stats, nil
}

// page is a single page's text plus the byte length of the separator that
// followed it in the original extracted text (1 for a form-feed, 0 for the last
// segment if it had no trailing \f).
type page struct {
	text   string
	sepLen int
}

func splitPages(text string) []page {
	var out []page
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == formFeed {
			out = append(out, page{text: text[start:i], sepLen: 1})
			start = i + 1
		}
	}
	out = append(out, page{text: text[start:], sepLen: 0})
	return out
}

// paragraph is a paragraph's text plus its byte offset within its page.
type paragraph struct {
	text   string
	offset int
}

// splitParagraphs splits a page into paragraphs on blank lines (\n\n),
// preserving each paragraph's byte offset within the page so chunk byte spans
// stay accurate. The separator bytes are attributed to neither paragraph (they
// fall in the gap), which is fine: re-slicing the extracted text by a chunk's
// [start:end] still reproduces the chunk text exactly because each chunk lives
// entirely within one paragraph's span.
func splitParagraphs(pageText string) []paragraph {
	var out []paragraph
	sep := "\n\n"
	start := 0
	for {
		idx := strings.Index(pageText[start:], sep)
		if idx < 0 {
			out = append(out, paragraph{text: pageText[start:], offset: start})
			break
		}
		abs := start + idx
		out = append(out, paragraph{text: pageText[start:abs], offset: start})
		start = abs + len(sep)
	}
	return out
}

func median(xs []int) int {
	if len(xs) == 0 {
		return 0
	}
	s := append([]int(nil), xs...)
	sort.Ints(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}
