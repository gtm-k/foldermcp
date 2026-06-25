//go:build cgo

package walker

import (
	"crypto/sha256"
	"io"
	"os"
)

// HashFile returns the SHA256 of the file at path, streamed via io.Copy.
// Streaming keeps RSS bounded for large files (no mmap, no full-read).
func HashFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}
