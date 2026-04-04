package workspace

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"strings"
)

// HashFile computes the SHA-256 hash of the file at path and returns it in
// the format "sha256:<hex>".
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file for hashing: %w", err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash file contents: %w", err)
	}

	return fmt.Sprintf("sha256:%x", h.Sum(nil)), nil
}

// VerifyHash computes the hash of the file at path and returns true if it
// matches the expectedHash. The expectedHash should be in "sha256:<hex>" format.
func VerifyHash(path, expectedHash string) (bool, error) {
	actual, err := HashFile(path)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(actual, expectedHash), nil
}
