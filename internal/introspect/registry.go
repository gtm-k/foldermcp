package introspect

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gobwas/glob"
)

// Registry manages introspector plugins and routes files to the right parser.
type Registry struct {
	plugins []IntrospectorPlugin
}

// NewRegistry creates a Registry with all built-in introspector plugins registered.
func NewRegistry() *Registry {
	r := &Registry{}
	r.Register(&PythonIntrospector{})
	r.Register(&OpenAPIIntrospector{})
	return r
}

// Register adds an introspector plugin to the registry.
func (r *Registry) Register(p IntrospectorPlugin) {
	r.plugins = append(r.plugins, p)
}

// ScanDirectory walks dir, applies include/exclude glob patterns, and routes
// each matching file to the first plugin that can handle it. Returns the
// aggregated tool metadata from all matched files.
func (r *Registry) ScanDirectory(dir string, includes, excludes []string) ([]ToolMetadata, error) {
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
				tools, extractErr := plugin.ExtractTools(path)
				if extractErr != nil {
					return fmt.Errorf("extract tools from %s: %w", path, extractErr)
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

	return allTools, nil
}

// compileGlobs compiles a slice of glob pattern strings into compiled matchers.
func compileGlobs(patterns []string) ([]glob.Glob, error) {
	compiled := make([]glob.Glob, 0, len(patterns))
	for _, p := range patterns {
		g, err := glob.Compile(p, '/')
		if err != nil {
			return nil, fmt.Errorf("invalid glob pattern %q: %w", p, err)
		}
		compiled = append(compiled, g)
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
