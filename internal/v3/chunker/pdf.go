//go:build cgo

package chunker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
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
	// ErrExtractionEmpty means a binary-document extractor SUCCEEDED but yielded
	// ZERO chunks (a PDF whose pdftotext output is only form-feeds/whitespace, or
	// a docx/odt with no text). Distinct from ErrExtractionQuality (which means
	// the document had text but too little to be digital-native): empty means
	// there is literally nothing to index, so the file must be marked failed
	// rather than silently counted done-with-zero-chunks and dropped from
	// PendingFiles forever (F1). Surfaced as error_message='extraction_empty'.
	ErrExtractionEmpty = errors.New("extraction_empty")
	// ErrExtractionTimeout means the extractor subprocess exceeded its per-file
	// deadline (a malformed/huge PDF can hang pdftotext). Surfaced as
	// error_message='extraction_timeout'.
	ErrExtractionTimeout = errors.New("extraction_timeout")
	// ErrExtractionOversize means the extractor's stdout exceeded the byte cap
	// (a PDF that expands to gigabytes of text would OOM the single-goroutine
	// indexer). Surfaced as error_message='extraction_oversize'.
	ErrExtractionOversize = errors.New("extraction_oversize")
	// ErrTooManyChunks means a single document produced more chunks than the
	// per-document cap (one large docx of tiny paragraphs can mint 100k+
	// chunks+embeddings in a single transaction). Surfaced as
	// error_message='too_many_chunks'.
	ErrTooManyChunks = errors.New("too_many_chunks")
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
	// extractTimeout caps a single pdftotext invocation (F2). A malformed or
	// pathological PDF can make poppler spin forever; the single-goroutine
	// indexer would then hang on one file. 60s is generous for any real
	// digital-native document yet bounds the worst case. Overridable later via
	// config if a corpus needs it.
	extractTimeout = 60 * time.Second
	// maxExtractedTextBytes caps the stdout we buffer from an extractor (F2). A
	// decompression-bomb PDF can expand to gigabytes of text and OOM the
	// single-goroutine indexer. 128 MB is ~4x the 32 MB per-file source read cap
	// — comfortably above any legitimate document's extracted text while still
	// bounding memory.
	maxExtractedTextBytes = 128 * 1024 * 1024
	// maxChunksPerDoc caps the chunks one document may mint (F3). A 30 MB docx of
	// tiny paragraphs can produce 100k+ chunks+embeddings in a single
	// transaction, blowing out memory and the embedding budget. 10000 chunks is
	// far beyond any real document (a 1000-page book chunks to a few thousand)
	// yet bounds the pathological case.
	maxChunksPerDoc = 10000
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

// cappedBuffer is an io.Writer that accumulates up to limit bytes and then
// reports overflow (F2). It never grows unbounded, so a decompression-bomb PDF
// cannot OOM the indexer: once the cap is hit we stop copying and surface
// ErrExtractionOversize.
type cappedBuffer struct {
	buf      bytes.Buffer
	limit    int
	overflow bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if c.overflow {
		// Pretend to consume so the child's pipe never blocks. Nothing kills
		// pdftotext early on overflow — the overflow is surfaced as
		// ErrExtractionOversize AFTER cmd.Run returns (see extractPDFText), and the
		// 60s extractTimeout bounds the worst case if the child keeps producing.
		return len(p), nil
	}
	remaining := c.limit - c.buf.Len()
	if len(p) > remaining {
		c.buf.Write(p[:remaining])
		c.overflow = true
		return len(p), nil
	}
	return c.buf.Write(p)
}

// extractPDFText runs `pdftotext -layout -- <path> -` once and returns the
// extracted text (form-feed page separators preserved). Zero network calls —
// it is a local subprocess. ctx is threaded so the runner can cancel, and a
// per-file timeout bounds a pathological PDF (F2). stdout is bounded to
// maxExtractedTextBytes (F2). The "--" terminator (F6) stops a path beginning
// with "-" from being parsed as a pdftotext flag (argument injection). Returns:
//   - ErrDependencyMissing if the binary is absent,
//   - ErrExtractionTimeout if the subprocess exceeds extractTimeout,
//   - ErrExtractionOversize if stdout exceeds the byte cap,
//
// so the runner can mark the file failed visibly under each marker.
func extractPDFText(ctx context.Context, path string, cfg PDFConfig) (string, error) {
	bin, err := pdftotextBinary(cfg)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, extractTimeout)
	defer cancel()

	// -layout preserves the physical layout; "--" ends option parsing (F6) so a
	// "-"-leading path is treated as a path; "-" writes to stdout.
	cmd := exec.CommandContext(ctx, bin, "-layout", "--", path, "-")
	out := &cappedBuffer{limit: maxExtractedTextBytes}
	var stderr bytes.Buffer
	cmd.Stdout = out
	cmd.Stderr = &stderr
	// WaitDelay ensures that when the context is cancelled (timeout) and the
	// process is killed, Wait still returns promptly even if the child left a
	// pipe open — otherwise a wedged child could block us indefinitely.
	cmd.WaitDelay = 5 * time.Second

	runErr := cmd.Run()
	if out.overflow {
		return "", fmt.Errorf("pdftotext %s: %w (>%d bytes)", path, ErrExtractionOversize, maxExtractedTextBytes)
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("pdftotext %s: %w (>%s)", path, ErrExtractionTimeout, extractTimeout)
	}
	if runErr != nil {
		// Propagate caller cancellation as the context error (not a per-file
		// extraction failure) so the runner treats it as a shutdown, not a bad file.
		if cerr := ctx.Err(); errors.Is(cerr, context.Canceled) {
			return "", cerr
		}
		return "", fmt.Errorf("pdftotext %s: %w (%s)", path, runErr, strings.TrimSpace(stderr.String()))
	}
	return out.buf.String(), nil
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
func ChunkPDF(ctx context.Context, path, title string, cfg Config, counter Counter, pdfCfg PDFConfig) ([]ExtractedChunk, ExtractStats, error) {
	text, err := extractPDFText(ctx, path, pdfCfg)
	if err != nil {
		return nil, ExtractStats{}, err
	}
	return chunkExtractedPages(text, title, pdfExtractorVersion, cfg, counter, true)
}

// chunkExtractedPages is the shared page-anchored chunking core used by ChunkPDF
// (pages already separated by form-feed) and, after the office extractor
// synthesizes a single-page extraction, by ChunkOffice. It splits text on
// form-feed into pages and chunks each page independently so no chunk spans a
// page boundary. qualityGate enables the digital-native <200-chars/page guard
// (PDF only — a legitimately short office document must still index, so office
// passes false).
func chunkExtractedPages(text, title, extractorVersion string, cfg Config, counter Counter, qualityGate bool) ([]ExtractedChunk, ExtractStats, error) {
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
				// Per-document chunk cap (F3): bound the chunks+embeddings one
				// document can mint in a single transaction. Surface a marker
				// rather than minting 100k+ rows. Checked inside the inner loop so
				// we stop as soon as the cap is crossed, never building the full
				// pathological slice first.
				if len(out) > maxChunksPerDoc {
					return nil, stats, fmt.Errorf("%w (>%d)", ErrTooManyChunks, maxChunksPerDoc)
				}
			}
		}
	}

	stats.Pages = pageNum
	stats.CharsPerPageMedian = median(charCounts)

	// Digital-native quality gate (D18): an image-only PDF extracts almost no
	// text; flag it visibly rather than indexing an empty document. Applies only
	// to PDFs that actually had pages (pageNum > 0) and produced text — a
	// truly-empty extraction is handled by the zero-chunk guard below.
	if qualityGate && pageNum > 0 && stats.CharsPerPageMedian < minCharsPerPage {
		return nil, stats, ErrExtractionQuality
	}
	// Zero-chunk guard (F1): a successful extraction that yields NO chunks (a PDF
	// whose text is only form-feeds/whitespace so pageNum==0 and the quality gate
	// is skipped; an office doc with no text since office passes qualityGate=false)
	// must fail observably. Otherwise the file is marked done with zero chunks,
	// counted indexed, removed from PendingFiles forever, and silently unsearchable.
	if len(out) == 0 {
		return nil, stats, ErrExtractionEmpty
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
