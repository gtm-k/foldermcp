package introspect

import (
	"context"
	"fmt"
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

	walkErr := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
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

	// Warn about duplicate tool names.
	seen := make(map[string]string) // name -> source file
	for _, t := range allTools {
		if prevFile, exists := seen[t.Name]; exists {
			logWarning("duplicate tool name %q: %s overwrites %s", t.Name, t.SourceFile, prevFile)
		}
		seen[t.Name] = t.SourceFile
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
