package introspect

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/gobwas/glob"
)

// logWarning writes a warning message to stderr. This is used instead of
// propagating per-file errors so that scanning continues gracefully.
func logWarning(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "WARNING: "+format+"\n", args...)
}

// Registry manages introspector plugins and routes files to the right parser.
type Registry struct {
	plugins []IntrospectorPlugin
}

// NewRegistry creates a Registry with all built-in introspector plugins registered.
func NewRegistry() *Registry {
	r := &Registry{}
	r.Register(&PythonIntrospector{})
	r.Register(&OpenAPIIntrospector{})
	r.Register(&TypeScriptIntrospector{})
	r.Register(&ShellIntrospector{})
	return r
}

// Register adds an introspector plugin to the registry.
func (r *Registry) Register(p IntrospectorPlugin) {
	r.plugins = append(r.plugins, p)
}

// ScanDirectory walks dir, applies include/exclude glob patterns, and routes
// each matching file to the first plugin that can handle it. Returns the
// aggregated tool metadata from all matched files.
func (r *Registry) ScanDirectory(ctx context.Context, dir string, includes, excludes []string) ([]ToolMetadata, error) {
	includeGlobs, err := compileGlobs(includes)
	if err != nil {
		return nil, fmt.Errorf("compile include globs: %w", err)
	}
	excludeGlobs, err := compileGlobs(excludes)
	if err != nil {
		return nil, fmt.Errorf("compile exclude globs: %w", err)
	}

	var allTools []ToolMetadata

	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		// Skip symlinks explicitly for safety.
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}

		// Get relative path for glob matching (use forward slashes for consistency).
		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)

		// Check include patterns -- if any are specified, the file must match at least one.
		if len(includeGlobs) > 0 && !matchesAny(relPath, includeGlobs) {
			return nil
		}

		// Check exclude patterns -- if the file matches any, skip it.
		if matchesAny(relPath, excludeGlobs) {
			return nil
		}

		// Route to the first matching plugin.
		for _, plugin := range r.plugins {
			if plugin.CanHandle(path) {
				tools, extractErr := plugin.ExtractTools(ctx, path)
				if extractErr != nil {
					logWarning("extract tools from %s: %v", path, extractErr)
					break
				}
				allTools = append(allTools, tools...)
				break
			}
		}
		return nil
	})

	if walkErr != nil {
		return nil, fmt.Errorf("walk directory %s: %w", dir, walkErr)
	}

	// Deduplicate tool names by appending a numeric suffix.
	seen := make(map[string]int)
	for i, t := range allTools {
		if count, exists := seen[t.Name]; exists {
			logWarning("duplicate tool name %q from %s (also in previous file); renaming to %s_%d", t.Name, t.SourceFile, t.Name, count+1)
			allTools[i].Name = fmt.Sprintf("%s_%d", t.Name, count+1)
			seen[t.Name] = count + 1
		} else {
			seen[t.Name] = 1
		}
	}

	return allTools, nil
}

// compileGlobs compiles a slice of glob pattern strings into compiled matchers.
// For patterns starting with "**/" it also compiles the suffix so that
// root-level files are matched (e.g. "**/*.py" also matches "foo.py").
func compileGlobs(patterns []string) ([]glob.Glob, error) {
	compiled := make([]glob.Glob, 0, len(patterns))
	for _, p := range patterns {
		g, err := glob.Compile(p, '/')
		if err != nil {
			return nil, fmt.Errorf("invalid glob pattern %q: %w", p, err)
		}
		compiled = append(compiled, g)

		// "**/" should match zero or more directories, so also match the
		// suffix for root-level files.
		if strings.HasPrefix(p, "**/") {
			suffix := strings.TrimPrefix(p, "**/")
			sg, err := glob.Compile(suffix, '/')
			if err != nil {
				return nil, fmt.Errorf("invalid glob pattern %q (suffix): %w", suffix, err)
			}
			compiled = append(compiled, sg)
		}
	}
	return compiled, nil
}

// matchesAny returns true if the path matches at least one of the compiled globs.
func matchesAny(path string, globs []glob.Glob) bool {
	for _, g := range globs {
		if g.Match(path) {
			return true
		}
	}
	return false
}
