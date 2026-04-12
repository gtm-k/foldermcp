//go:build cgo

package walker

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// ClassifyFile opens the file, sniffs the first 512 bytes, and returns
// (mime, content_class). content_class is one of:
//
//	code | document | image | media | data | unknown
func ClassifyFile(path string) (string, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	mime := http.DetectContentType(buf[:n])

	ext := strings.ToLower(filepath.Ext(path))
	class := classFromExtOrMime(ext, mime)
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
