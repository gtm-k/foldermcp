package introspect

// ToolMetadata describes a discovered tool from source code introspection.
type ToolMetadata struct {
	Name        string
	SourceFile  string
	Description string
	InputSchema string // JSON Schema as string
	Risk        string // read_only, side_effects, destructive, network
	Language    string // python, typescript, openapi, shell
}

// Dependency represents an imported external package.
type Dependency struct {
	ImportName  string
	PackageName string // may be empty if unresolved
}

// IntrospectorPlugin defines the interface for language-specific tool discovery.
type IntrospectorPlugin interface {
	// CanHandle returns true if the plugin can handle the given file path.
	CanHandle(filePath string) bool

	// ExtractTools parses the file and returns tool metadata.
	ExtractTools(filePath string) ([]ToolMetadata, error)

	// InferDependencies inspects the file for import/dependency declarations.
	InferDependencies(filePath string) ([]Dependency, error)
}
