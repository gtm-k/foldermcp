package cache

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/foldermcp/foldermcp/internal/workspace"
)

// Cache provides a content-addressed local cache for tool source files.
// It eliminates repeated NAS reads by storing local copies keyed by the
// SHA-256 of the source path, and validates freshness using file content hashes.
type Cache struct {
	dir string // local cache directory (workspace.CacheDir())
}

// New creates a Cache rooted at dir. The directory is created if it does not
// exist.
func New(dir string) *Cache {
	return &Cache{dir: dir}
}

// Get returns the path to a local cached copy of sourcePath. If a cached copy
// already exists and its content hash matches the source, the cached path is
// returned immediately. Otherwise the file is copied from source to cache.
func (c *Cache) Get(sourcePath string) (string, error) {
	cachedPath := c.cachedPath(sourcePath)

	// Check if cached copy already exists.
	if _, err := os.Stat(cachedPath); err == nil {
		// Cached copy exists. Verify it matches the source.
		sourceHash, err := workspace.HashFile(sourcePath)
		if err != nil {
			return "", fmt.Errorf("hash source file: %w", err)
		}
		cachedHash, err := workspace.HashFile(cachedPath)
		if err != nil {
			// Cached file is corrupt; re-copy below.
			goto recopy
		}
		if sourceHash == cachedHash {
			return cachedPath, nil
		}
	}

recopy:
	// Copy source to cache.
	if err := c.copyFile(sourcePath, cachedPath); err != nil {
		return "", fmt.Errorf("copy to cache: %w", err)
	}
	return cachedPath, nil
}

// Invalidate removes the cached copy of sourcePath, forcing the next Get to
// re-read from the source.
func (c *Cache) Invalidate(sourcePath string) error {
	cachedPath := c.cachedPath(sourcePath)

	// Remove the file. If it doesn't exist, that's fine.
	if err := os.Remove(cachedPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove cached file: %w", err)
	}
	return nil
}

// cachedPath computes the local cache path for a given source path.
// Layout: cacheDir/<pathHash>/<originalFilename>
func (c *Cache) cachedPath(sourcePath string) string {
	h := sha256.Sum256([]byte(sourcePath))
	dirName := fmt.Sprintf("%x", h[:])[:16]
	return filepath.Join(c.dir, dirName, filepath.Base(sourcePath))
}

// copyFile copies src to dst, creating parent directories as needed.
func (c *Cache) copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}

	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer func() { _ = in.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create cached file: %w", err)
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy data: %w", err)
	}
	return out.Close()
}
