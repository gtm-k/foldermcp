package introspect

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ShellIntrospector extracts tool metadata from shell scripts (.sh, .bash).
// Each script becomes a single tool that accepts free-form command line arguments.
type ShellIntrospector struct{}

// CanHandle returns true for files with .sh or .bash extensions.
func (s *ShellIntrospector) CanHandle(filePath string) bool {
	ext := filepath.Ext(filePath)
	return ext == ".sh" || ext == ".bash"
}

// ExtractTools parses a shell script and returns a single tool whose name is
// derived from the filename (minus extension). The description is taken from
// the first comment line that is not a shebang (#!). If no suitable comment
// is found, a default "Runs <filename>" description is used.
func (s *ShellIntrospector) ExtractTools(filePath string) ([]ToolMetadata, error) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	base := filepath.Base(filePath)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)

	description, err := extractShellDescription(absPath, base)
	if err != nil {
		return nil, err
	}

	inputSchema := `{"type":"object","properties":{"args":{"type":"string","description":"Command line arguments"}}}`

	return []ToolMetadata{
		{
			Name:        name,
			SourceFile:  absPath,
			Description: description,
			InputSchema: inputSchema,
			Risk:        "side_effects",
			Language:    "shell",
		},
	}, nil
}

// InferDependencies returns nil for shell scripts. Shell scripts manage their
// own dependencies through the system PATH and package managers.
func (s *ShellIntrospector) InferDependencies(filePath string) ([]Dependency, error) {
	return nil, nil
}

// extractShellDescription reads the file and returns the text of the first
// comment line that is not a shebang. Falls back to "Runs <filename>".
func extractShellDescription(absPath, filename string) (string, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", absPath, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines.
		if line == "" {
			continue
		}

		// Skip shebang lines.
		if strings.HasPrefix(line, "#!") {
			continue
		}

		// A comment line (not shebang) provides the description.
		if strings.HasPrefix(line, "#") {
			desc := strings.TrimSpace(strings.TrimPrefix(line, "#"))
			if desc != "" {
				return desc, nil
			}
			continue
		}

		// Non-comment, non-empty line reached without finding a description.
		break
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read %s: %w", absPath, err)
	}

	return "Runs " + filename, nil
}
