//go:build cgo

package walker

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// binarySniffBytes is the prefix length sampled for content-based binary
// detection. 8 KB matches git's NUL-scan window (git treats a NUL in the first
// 8000 bytes as binary) — large enough to catch a binary body that starts after
// the 512-byte http.DetectContentType window, cheap enough that the extra read
// is dwarfed by the full-file SHA256 the walker already computes per file.
const binarySniffBytes = 8192

// ClassifyFile opens the file, samples its leading bytes, and returns
// (mime, content_class). content_class is one of:
//
//	code | document | image | media | data | unknown
//
// Classification is extension-and-mime first, then a content-based override:
// an otherwise-indexable file (code/document) whose sampled bytes look binary
// (IsBinaryContent) is downgraded to 'unknown' so the pipeline's existing
// non-indexable skip drops it instead of prose-chunking garbage. This closes
// the cases http.DetectContentType cannot: a binary blob with a text extension
// (the extension switch would otherwise win) and a binary body that turns
// binary beyond the 512-byte sniff window (DetectContentType only sees the
// first 512 bytes, so a clean-text header masks a binary tail).
//
// Binary documents (.pdf/.docx/.odt) are exempt from the override: they ARE
// binary (a ZIP/PDF container has NULs in its first 8 KB) but they have a
// dedicated downstream path — the pipeline marks them 'binary_document_pending_m2'
// and M2 Phase 4 will extract their text. Downgrading them to 'unknown' here
// would strip that marker and silently route real PDFs/Office docs around the
// future extractor.
func ClassifyFile(path string) (string, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, binarySniffBytes)
	n, err := io.ReadFull(f, buf)
	// Short reads are normal: any file smaller than binarySniffBytes yields
	// io.EOF (empty) or io.ErrUnexpectedEOF (partial). Only an unexpected I/O
	// error is fatal.
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", "", err
	}
	sample := buf[:n]

	// http.DetectContentType only inspects the first 512 bytes.
	sniffLen := len(sample)
	if sniffLen > 512 {
		sniffLen = 512
	}
	mime := http.DetectContentType(sample[:sniffLen])

	ext := strings.ToLower(filepath.Ext(path))
	class := classFromExtOrMime(ext, mime)

	// Content-based override (Q2): only the indexable classes are at risk of
	// being prose-chunked, and only those carry information worth preserving
	// for the false case — image/media/data keep their honest labels. Binary
	// documents are exempt (see doc comment) so they keep their dedicated
	// 'binary_document_pending_m2' pipeline path.
	if (class == "code" || (class == "document" && !IsBinaryDocumentExt(path))) && IsBinaryContent(sample) {
		class = "unknown"
	}
	return mime, class, nil
}

func classFromExtOrMime(ext, mime string) string {
	// Extension takes precedence for text-like files because
	// DetectContentType returns "text/plain; charset=..." for all source code.
	switch ext {
	case ".py", ".go", ".js", ".ts", ".rs", ".java", ".c", ".cc", ".cpp", ".h", ".hpp":
		return "code"
	case ".md", ".txt", ".rst", ".org", ".tex", ".pdf", ".docx", ".odt":
		return "document"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".heic", ".tiff":
		return "image"
	case ".mp4", ".mkv", ".mov", ".avi", ".mp3", ".wav", ".flac", ".ogg", ".webm":
		return "media"
	case ".csv", ".tsv", ".json", ".yaml", ".yml", ".xml", ".sqlite":
		return "data"
	}
	switch {
	case strings.HasPrefix(mime, "text/"):
		return "document"
	case strings.HasPrefix(mime, "image/"):
		return "image"
	case strings.HasPrefix(mime, "video/"), strings.HasPrefix(mime, "audio/"):
		return "media"
	}
	return "unknown"
}
