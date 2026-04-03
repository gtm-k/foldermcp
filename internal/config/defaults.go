package config

// DefaultConfig returns a Config populated with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Version: CurrentSchemaVersion,
		Scan: ScanConfig{
			Include: []string{
				"*.py",
				"*.ts",
				"*.js",
				"*.yaml",
				"*.yml",
				"*.sh",
			},
			Exclude: []string{
				"tests/**",
				"__pycache__/**",
				"node_modules/**",
				".git/**",
				".foldermcp/**",
			},
		},
		Tools: make(map[string]ToolConfig),
		Dependencies: DependencyConfig{
			Python: []string{},
			Node:   []string{},
		},
		ToolRouting: ToolRoutingConfig{
			MaxToolsPerContext: 20,
			Strategy:           "profile",
			Profiles:           make(map[string][]string),
		},
	}
}
