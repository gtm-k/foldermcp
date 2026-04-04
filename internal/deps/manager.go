package deps

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// ImportedPackage represents a Python import discovered by introspection.
type ImportedPackage struct {
	ImportName string
}

// ResolvedPackage is an import that has been mapped to a PyPI package name.
type ResolvedPackage struct {
	ImportName string
	PyPIName   string
	Resolved   bool // true if found in the known-packages mapping
}

// ResolveResult is the output of dependency resolution for a project directory.
type ResolveResult struct {
	Strategy   string            // "requirements.txt", "pyproject.toml", "inferred", "none"
	Packages   []ResolvedPackage // all resolved packages
	Unresolved []string          // import names that could not be mapped
}

// Manager handles Python dependency resolution and virtual environment creation.
type Manager struct {
	mapping *PackageMapping
}

// NewManager creates a Manager with the embedded known-packages mapping.
// It panics if the embedded mapping cannot be loaded (should never happen
// since the JSON is compiled in).
func NewManager() *Manager {
	pm, err := LoadMapping()
	if err != nil {
		panic(fmt.Sprintf("deps: load embedded mapping: %v", err))
	}
	return &Manager{mapping: pm}
}

// Resolve determines the dependency strategy for a project directory and
// returns the resolved packages. Priority order:
//
//  1. requirements.txt — parse package names from requirements file
//  2. pyproject.toml — extract [project].dependencies
//  3. inferred — map the provided imports list through known packages
//  4. none — no dependency information available
func (m *Manager) Resolve(dir string, imports []ImportedPackage) (*ResolveResult, error) {
	// 1. Check for requirements.txt
	reqPath := filepath.Join(dir, "requirements.txt")
	if _, err := os.Stat(reqPath); err == nil {
		pkgs, err := parseRequirementsTxt(reqPath)
		if err != nil {
			return nil, fmt.Errorf("parse requirements.txt: %w", err)
		}
		resolved := make([]ResolvedPackage, len(pkgs))
		for i, name := range pkgs {
			resolved[i] = ResolvedPackage{
				ImportName: name,
				PyPIName:   name,
				Resolved:   true,
			}
		}
		return &ResolveResult{
			Strategy: "requirements.txt",
			Packages: resolved,
		}, nil
	}

	// 2. Check for pyproject.toml
	pyprojectPath := filepath.Join(dir, "pyproject.toml")
	if _, err := os.Stat(pyprojectPath); err == nil {
		pkgs, err := parsePyprojectToml(pyprojectPath)
		if err != nil {
			return nil, fmt.Errorf("parse pyproject.toml: %w", err)
		}
		resolved := make([]ResolvedPackage, len(pkgs))
		for i, name := range pkgs {
			resolved[i] = ResolvedPackage{
				ImportName: name,
				PyPIName:   name,
				Resolved:   true,
			}
		}
		return &ResolveResult{
			Strategy: "pyproject.toml",
			Packages: resolved,
		}, nil
	}

	// 3. Infer from imports
	if len(imports) > 0 {
		packages, unresolved := m.MapImports(imports)
		return &ResolveResult{
			Strategy:   "inferred",
			Packages:   packages,
			Unresolved: unresolved,
		}, nil
	}

	// 4. Nothing
	return &ResolveResult{
		Strategy: "none",
	}, nil
}

// MapImports maps a list of imported packages through the known-packages
// mapping, returning resolved packages and a list of unresolved import names.
func (m *Manager) MapImports(imports []ImportedPackage) ([]ResolvedPackage, []string) {
	var packages []ResolvedPackage
	var unresolved []string

	for _, imp := range imports {
		pypiName, found := m.mapping.Resolve(imp.ImportName)
		packages = append(packages, ResolvedPackage{
			ImportName: imp.ImportName,
			PyPIName:   pypiName,
			Resolved:   found,
		})
		if !found {
			unresolved = append(unresolved, imp.ImportName)
		}
	}

	return packages, unresolved
}

// CreateVenv creates a Python virtual environment at dir/.foldermcp/venv
// using uv and installs the given packages. It returns the path to the
// created venv directory.
func (m *Manager) CreateVenv(dir string, packages []string) (string, error) {
	uvPath, err := exec.LookPath("uv")
	if err != nil {
		return "", fmt.Errorf("uv not found in PATH: %w", err)
	}

	venvDir := filepath.Join(dir, ".foldermcp", "venv")

	// Create the .foldermcp directory if it doesn't exist.
	if err := os.MkdirAll(filepath.Dir(venvDir), 0755); err != nil {
		return "", fmt.Errorf("create .foldermcp dir: %w", err)
	}

	// Create the venv.
	cmd := exec.Command(uvPath, "venv", venvDir)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("uv venv failed: %w\noutput: %s", err, string(out))
	}

	// Install packages if any.
	if len(packages) > 0 {
		args := append([]string{"pip", "install", "--python", venvDir}, packages...)
		cmd = exec.Command(uvPath, args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("uv pip install failed: %w\noutput: %s", err, string(out))
		}
	}

	return venvDir, nil
}

// parseRequirementsTxt reads a requirements.txt file and extracts package names,
// stripping version specifiers, comments, and options.
func parseRequirementsTxt(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	// Matches the package name at the start of a requirement line.
	// Package names consist of letters, digits, hyphens, underscores, and dots.
	pkgNameRe := regexp.MustCompile(`^([A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?)`)

	var packages []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines, comments, and options.
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") {
			continue
		}

		if m := pkgNameRe.FindString(line); m != "" {
			packages = append(packages, m)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return packages, nil
}

// pyprojectToml represents the subset of pyproject.toml we care about.
type pyprojectToml struct {
	Project struct {
		Dependencies []string `toml:"dependencies"`
	} `toml:"project"`
}

// parsePyprojectToml parses a pyproject.toml using a proper TOML parser and
// extracts the [project].dependencies list, stripping version specifiers to
// return bare package names.
func parsePyprojectToml(path string) ([]string, error) {
	var pp pyprojectToml
	if _, err := toml.DecodeFile(path, &pp); err != nil {
		return nil, err
	}

	// Same regex for package name extraction (strip version specifiers).
	pkgNameRe := regexp.MustCompile(`^([A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?)`)

	var packages []string
	for _, spec := range pp.Project.Dependencies {
		spec = strings.TrimSpace(spec)
		if name := pkgNameRe.FindString(spec); name != "" {
			packages = append(packages, name)
		}
	}

	return packages, nil
}
