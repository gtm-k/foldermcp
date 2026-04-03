package introspect

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// OpenAPIIntrospector extracts tool metadata from OpenAPI specification files.
type OpenAPIIntrospector struct{}

// CanHandle returns true for .yaml, .yml, .json files, excluding foldermcp.yaml.
func (o *OpenAPIIntrospector) CanHandle(filePath string) bool {
	base := filepath.Base(filePath)
	if base == "foldermcp.yaml" {
		return false
	}
	ext := strings.ToLower(filepath.Ext(filePath))
	return ext == ".yaml" || ext == ".yml" || ext == ".json"
}

// ExtractTools parses an OpenAPI spec and returns tool metadata for each operation.
func (o *OpenAPIIntrospector) ExtractTools(filePath string) ([]ToolMetadata, error) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to load OpenAPI spec %s: %w", filePath, err)
	}

	if err := doc.Validate(context.Background()); err != nil {
		return nil, fmt.Errorf("invalid OpenAPI spec %s: %w", filePath, err)
	}

	var tools []ToolMetadata

	for path, pathItem := range doc.Paths.Map() {
		operations := map[string]*openapi3.Operation{
			"GET":     pathItem.Get,
			"POST":    pathItem.Post,
			"PUT":     pathItem.Put,
			"PATCH":   pathItem.Patch,
			"DELETE":  pathItem.Delete,
			"HEAD":    pathItem.Head,
			"OPTIONS": pathItem.Options,
		}

		for method, op := range operations {
			if op == nil {
				continue
			}

			name := op.OperationID
			if name == "" {
				name = strings.ToLower(method) + sanitizePath(path)
			}

			description := op.Summary
			if description == "" {
				description = op.Description
			}
			if description == "" {
				description = method + " " + path
			}

			schema := buildInputSchema(op, method)
			schemaJSON, err := json.Marshal(schema)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal schema for %s %s: %w", method, path, err)
			}

			tools = append(tools, ToolMetadata{
				Name:        name,
				SourceFile:  filePath,
				Description: description,
				InputSchema: string(schemaJSON),
				Risk:        riskFromMethod(method),
				Language:    "openapi",
			})
		}
	}

	return tools, nil
}

// InferDependencies returns nil -- OpenAPI specs have no code dependencies.
func (o *OpenAPIIntrospector) InferDependencies(filePath string) ([]Dependency, error) {
	return nil, nil
}

// riskFromMethod maps HTTP methods to risk levels.
func riskFromMethod(method string) string {
	switch strings.ToUpper(method) {
	case "GET", "HEAD", "OPTIONS":
		return "read_only"
	case "DELETE":
		return "destructive"
	default:
		return "side_effects"
	}
}

// sanitizePath replaces /, {, } with underscores to make valid tool names.
func sanitizePath(path string) string {
	r := strings.NewReplacer("/", "_", "{", "_", "}", "_")
	return r.Replace(path)
}

// buildInputSchema creates a JSON Schema object from OpenAPI operation parameters
// and request body properties.
func buildInputSchema(op *openapi3.Operation, method string) map[string]interface{} {
	schema := map[string]interface{}{
		"type": "object",
	}
	properties := make(map[string]interface{})
	var required []string

	// Add parameters (path, query, header, cookie)
	for _, paramRef := range op.Parameters {
		if paramRef == nil || paramRef.Value == nil {
			continue
		}
		param := paramRef.Value
		propSchema := make(map[string]interface{})

		if param.Schema != nil && param.Schema.Value != nil {
			propSchema["type"] = param.Schema.Value.Type
			if param.Schema.Value.Description != "" {
				propSchema["description"] = param.Schema.Value.Description
			}
		}

		properties[param.Name] = propSchema

		if param.Required {
			required = append(required, param.Name)
		}
	}

	// Add request body properties (for POST, PUT, PATCH)
	if op.RequestBody != nil && op.RequestBody.Value != nil {
		rb := op.RequestBody.Value
		if content, ok := rb.Content["application/json"]; ok && content.Schema != nil && content.Schema.Value != nil {
			bodySchema := content.Schema.Value

			for propName, propRef := range bodySchema.Properties {
				if propRef == nil || propRef.Value == nil {
					continue
				}
				propSchema := make(map[string]interface{})
				propSchema["type"] = propRef.Value.Type
				if propRef.Value.Description != "" {
					propSchema["description"] = propRef.Value.Description
				}
				properties[propName] = propSchema
			}

			// Add required fields from the request body
			for _, req := range bodySchema.Required {
				required = append(required, req)
			}
		}
	}

	if len(properties) > 0 {
		schema["properties"] = properties
	}
	if len(required) > 0 {
		schema["required"] = required
	}

	return schema
}
