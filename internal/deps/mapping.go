package deps

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed known_packages.json
var knownPackagesJSON []byte

// PackageMapping resolves Python import names to their PyPI package names
// using a curated embedded mapping.
type PackageMapping struct {
	mapping map[string]string
}

// LoadMapping unmarshals the embedded known_packages.json and returns a
// PackageMapping ready for lookups.
func LoadMapping() (*PackageMapping, error) {
	var m map[string]string
	if err := json.Unmarshal(knownPackagesJSON, &m); err != nil {
		return nil, fmt.Errorf("unmarshal known_packages.json: %w", err)
	}
	return &PackageMapping{mapping: m}, nil
}

// Resolve looks up the given Python import name in the known-package mapping.
// If found, it returns the corresponding PyPI package name and true.
// If not found, it returns the import name itself as a best-guess and false.
func (pm *PackageMapping) Resolve(importName string) (packageName string, wasFound bool) {
	if pkg, ok := pm.mapping[importName]; ok {
		return pkg, true
	}
	return importName, false
}
