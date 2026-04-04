package introspect

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// OpenAPIIntrospector extracts tool metadata from OpenAPI specification files.
type OpenAPIIntrospector struct{}

// CanHandle returns true for .yaml, .yml, .json files that appear to contain
// an OpenAPI or Swagger specification. It excludes known non-OpenAPI files
// and performs a quick content sniff on the first 512 bytes.
func (o *OpenAPIIntrospector) CanHandle(filePath string) bool {
	lower := strings.ToLower(filePath)
	if strings.HasSuffix(lower, "foldermcp.yaml") || strings.HasSuffix(lower, "foldermcp.yml") {
		return false
	}
	if strings.HasSuffix(lower, "package.json") || strings.HasSuffix(lower, "tsconfig.json") || strings.HasSuffix(lower, "docker-compose.yml") || strings.HasSuffix(lower, "docker-compose.yaml") {
		return false
	}
	if !strings.HasSuffix(lower, ".yaml") && !strings.HasSuffix(lower, ".yml") && !strings.HasSuffix(lower, ".json") {
		return false
	}
	// Quick content sniff: check first 512 bytes for openapi/swagger key.
	data, err := os.ReadFile(filePath)
	if err != nil {
		return false
	}
	header := strings.ToLower(string(data[:min(len(data), 512)]))
	return strings.Contains(header, "openapi") || strings.Contains(header, "swagger")
}

// ExtractTools parses an OpenAPI spec and returns tool metadata for each operation.
func (o *OpenAPIIntrospector) ExtractTools(ctx context.Context, filePath string) ([]ToolMetadata, error) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(filePath)
	if err != nil {
		// Not a valid OpenAPI file — return empty list so the scan continues.
		return nil, nil
	}

	if err := doc.Validate(ctx); err != nil {
		// File loaded but is not a valid OpenAPI spec — return empty list.
		return nil, nil
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
func (o *OpenAPIIntrospector) InferDependencies(ctx context.Context, filePath string) ([]Dependency, error) {
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
			required = append(required, bodySchema.Required...)
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
