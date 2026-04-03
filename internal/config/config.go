package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// CurrentSchemaVersion is the supported config schema version.
const CurrentSchemaVersion = 1

// Config is the top-level configuration for FolderMCP.
type Config struct {
	Version      int                       `yaml:"version"`
	Scan         ScanConfig                `yaml:"scan"`
	Tools        map[string]ToolConfig     `yaml:"tools"`
	Dependencies DependencyConfig          `yaml:"dependencies"`
	ToolRouting  ToolRoutingConfig         `yaml:"tool_routing"`
}

// ScanConfig controls which files are included/excluded during scanning.
type ScanConfig struct {
	Include []string `yaml:"include"`
	Exclude []string `yaml:"exclude"`
}

// ToolConfig describes a single tool's configuration.
type ToolConfig struct {
	State       string `yaml:"state"`
	Description string `yaml:"description"`
	Risk        string `yaml:"risk"`
}

// DependencyConfig lists project dependencies by ecosystem.
type DependencyConfig struct {
	Python []string `yaml:"python"`
	Node   []string `yaml:"node"`
}

// ToolRoutingConfig controls how tools are selected and routed.
type ToolRoutingConfig struct {
	MaxToolsPerContext int                 `yaml:"max_tools_per_context"`
	Strategy           string              `yaml:"strategy"`
	Profiles           map[string][]string `yaml:"profiles"`
}

// configFileName is the name of the configuration file.
const configFileName = "foldermcp.yaml"

// Load reads foldermcp.yaml from dir. If the file does not exist, it returns
// the default configuration. It returns an error if the file exists but
// contains an unsupported schema version.
func Load(dir string) (*Config, error) {
	path := filepath.Join(dir, configFileName)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := DefaultConfig()
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if cfg.Version != CurrentSchemaVersion {
		return nil, fmt.Errorf("unsupported config version %d (expected %d)", cfg.Version, CurrentSchemaVersion)
	}

	// Ensure Tools map is never nil.
	if cfg.Tools == nil {
		cfg.Tools = make(map[string]ToolConfig)
	}

	// Ensure Profiles map is never nil.
	if cfg.ToolRouting.Profiles == nil {
		cfg.ToolRouting.Profiles = make(map[string][]string)
	}

	return &cfg, nil
}

// Save writes the configuration to foldermcp.yaml in the given directory.
func Save(dir string, cfg *Config) error {
	path := filepath.Join(dir, configFileName)

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	return nil
}
