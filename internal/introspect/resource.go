package introspect

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/gobwas/glob"
)

// maxResourceSizeBytes is the maximum file size for resource discovery (50 MB).
const maxResourceSizeBytes int64 = 50 * 1024 * 1024

// ResourceMetadata describes a discovered non-code file suitable for MCP resource exposure.
type ResourceMetadata struct {
	Name      string // filename without extension
	FilePath  string // absolute path
	MimeType  string // detected MIME type
	SizeBytes int64
	Type      string // "document", "image", "data", "config"
}

// mimeMap maps file extensions to MIME types.
var mimeMap = map[string]string{
	// Documents
	".pdf":  "application/pdf",
	".md":   "text/markdown",
	".txt":  "text/plain",
	".doc":  "application/msword",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",

	// Images
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".svg":  "image/svg+xml",
	".gif":  "image/gif",

	// Data
	".csv":     "text/csv",
	".json":    "application/json",
	".parquet": "application/octet-stream",
	".xlsx":    "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",

	// Config
	".yaml": "text/yaml",
	".yml":  "text/yaml",
	".toml": "text/toml",
}

// typeMap maps file extensions to resource types.
var typeMap = map[string]string{
	".pdf": "document", ".md": "document", ".txt": "document",
	".doc": "document", ".docx": "document",

	".png": "image", ".jpg": "image", ".jpeg": "image",
	".svg": "image", ".gif": "image",

	".csv": "data", ".json": "data", ".parquet": "data", ".xlsx": "data",

	".yaml": "config", ".yml": "config", ".toml": "config",
}

// codeExtensions lists extensions handled by tool introspectors that should be skipped.
var codeExtensions = map[string]bool{
	".py": true, ".ts": true, ".js": true, ".sh": true,
}

// ResourceIntrospector discovers non-code files for MCP resource exposure.
type ResourceIntrospector struct{}

// Discover walks dir, applying include/exclude patterns, and returns metadata
// for non-code files that match known resource types.
func (r *ResourceIntrospector) Discover(dir string, includes, excludes []string) ([]ResourceMetadata, error) {
	includeGlobs, err := compileGlobs(includes)
	if err != nil {
		return nil, fmt.Errorf("compile resource include globs: %w", err)
	}
	excludeGlobs, err := compileGlobs(excludes)
	if err != nil {
		return nil, fmt.Errorf("compile resource exclude globs: %w", err)
	}

	var resources []ResourceMetadata

	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		// Skip symlinks for safety.
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}

		// Skip .env files for security (SEC-02).
		baseName := filepath.Base(path)
		if strings.HasPrefix(baseName, ".env") {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))

		// Skip code files handled by tool introspectors.
		if codeExtensions[ext] {
			return nil
		}

		// Check if this extension is a known resource type.
		mime, knownMime := mimeMap[ext]
		resType, knownType := typeMap[ext]
		if !knownMime || !knownType {
			return nil
		}

		// Get relative path for glob matching.
		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return nil
		}
		relPath = filepath.ToSlash(relPath)

		// Apply include patterns.
		if len(includeGlobs) > 0 && !matchesAnyGlob(relPath, includeGlobs) {
			return nil
		}

		// Apply exclude patterns.
		if matchesAnyGlob(relPath, excludeGlobs) {
			return nil
		}

		// Get file info for size.
		info, err := d.Info()
		if err != nil {
			logWarning("stat resource %s: %v", path, err)
			return nil
		}

		// Skip files over 50 MB.
		if info.Size() > maxResourceSizeBytes {
			logWarning("skipping resource %s: file size %d bytes exceeds 50 MB limit", relPath, info.Size())
			return nil
		}

		absPath, err := filepath.Abs(path)
		if err != nil {
			absPath = path
		}

		// Use filename (already computed above for .env check) as the
		// resource name, including the extension to avoid collisions
		// (e.g. guide.md vs guide.pdf).

		resources = append(resources, ResourceMetadata{
			Name:      baseName,
			FilePath:  absPath,
			MimeType:  mime,
			SizeBytes: info.Size(),
			Type:      resType,
		})

		return nil
	})

	if walkErr != nil {
		return nil, fmt.Errorf("walk directory %s: %w", dir, walkErr)
	}

	return resources, nil
}

// matchesAnyGlob returns true if path matches at least one compiled glob.
func matchesAnyGlob(path string, globs []glob.Glob) bool {
	for _, g := range globs {
		if g.Match(path) {
			return true
		}
	}
	return false
}
