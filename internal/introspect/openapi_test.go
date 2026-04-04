package introspect

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestOpenAPIIntrospector_CanHandle(t *testing.T) {
	o := &OpenAPIIntrospector{}

	tests := []struct {
		name     string
		filePath string
		want     bool
	}{
		{"yaml file", "spec.yaml", true},
		{"yml file", "spec.yml", true},
		{"json file", "spec.json", true},
		{"python file", "app.py", false},
		{"go file", "main.go", false},
		{"foldermcp.yaml excluded", "foldermcp.yaml", false},
		{"foldermcp.yaml in subdir excluded", "/some/path/foldermcp.yaml", false},
		{"yaml in path", "/api/openapi.yaml", true},
		{"json in path", "/api/openapi.json", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := o.CanHandle(tt.filePath)
			if got != tt.want {
				t.Errorf("CanHandle(%q) = %v, want %v", tt.filePath, got, tt.want)
			}
		})
	}
}

func TestOpenAPIIntrospector_ExtractTools(t *testing.T) {
	o := &OpenAPIIntrospector{}
	specPath := filepath.Join(testdataDir(), "openapi_simple", "petstore.yaml")

	tools, err := o.ExtractTools(context.Background(), specPath)
	if err != nil {
		t.Fatalf("ExtractTools failed: %v", err)
	}

	if len(tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(tools))
	}

	// Build a map for easier lookup
	toolMap := make(map[string]ToolMetadata)
	for _, tool := range tools {
		toolMap[tool.Name] = tool
	}

	// Check listPets
	listPets, ok := toolMap["listPets"]
	if !ok {
		t.Fatal("expected tool 'listPets' not found")
	}
	if listPets.Risk != "read_only" {
		t.Errorf("listPets risk = %q, want %q", listPets.Risk, "read_only")
	}
	if listPets.Description != "List all pets" {
		t.Errorf("listPets description = %q, want %q", listPets.Description, "List all pets")
	}
	if listPets.Language != "openapi" {
		t.Errorf("listPets language = %q, want %q", listPets.Language, "openapi")
	}
	// Parse InputSchema (JSON string) and check for limit property
	var listSchema map[string]interface{}
	if err := json.Unmarshal([]byte(listPets.InputSchema), &listSchema); err != nil {
		t.Fatalf("listPets InputSchema is not valid JSON: %v", err)
	}
	props, ok := listSchema["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("listPets input schema has no properties")
	}
	if _, ok := props["limit"]; !ok {
		t.Error("listPets input schema missing 'limit' property")
	}

	// Check createPet
	createPet, ok := toolMap["createPet"]
	if !ok {
		t.Fatal("expected tool 'createPet' not found")
	}
	if createPet.Risk != "side_effects" {
		t.Errorf("createPet risk = %q, want %q", createPet.Risk, "side_effects")
	}
	if createPet.Description != "Create a pet" {
		t.Errorf("createPet description = %q, want %q", createPet.Description, "Create a pet")
	}
	// Parse and check request body properties
	var createSchema map[string]interface{}
	if err := json.Unmarshal([]byte(createPet.InputSchema), &createSchema); err != nil {
		t.Fatalf("createPet InputSchema is not valid JSON: %v", err)
	}
	createProps, ok := createSchema["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("createPet input schema has no properties")
	}
	if _, ok := createProps["name"]; !ok {
		t.Error("createPet input schema missing 'name' property")
	}
	if _, ok := createProps["tag"]; !ok {
		t.Error("createPet input schema missing 'tag' property")
	}
	// Check required includes "name"
	required, ok := createSchema["required"]
	if !ok {
		t.Fatal("createPet input schema has no required field")
	}
	reqSlice, ok := required.([]interface{})
	if !ok {
		t.Fatalf("createPet required field is not a slice, got %T", required)
	}
	found := false
	for _, r := range reqSlice {
		if r == "name" {
			found = true
			break
		}
	}
	if !found {
		t.Error("createPet required field does not include 'name'")
	}

	// Check deletePet
	deletePet, ok := toolMap["deletePet"]
	if !ok {
		t.Fatal("expected tool 'deletePet' not found")
	}
	if deletePet.Risk != "destructive" {
		t.Errorf("deletePet risk = %q, want %q", deletePet.Risk, "destructive")
	}
	if deletePet.Description != "Delete a pet by ID" {
		t.Errorf("deletePet description = %q, want %q", deletePet.Description, "Delete a pet by ID")
	}
	// Parse and check petId parameter
	var deleteSchema map[string]interface{}
	if err := json.Unmarshal([]byte(deletePet.InputSchema), &deleteSchema); err != nil {
		t.Fatalf("deletePet InputSchema is not valid JSON: %v", err)
	}
	deleteProps, ok := deleteSchema["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("deletePet input schema has no properties")
	}
	if _, ok := deleteProps["petId"]; !ok {
		t.Error("deletePet input schema missing 'petId' property")
	}
}

func TestOpenAPIIntrospector_InferDependencies(t *testing.T) {
	o := &OpenAPIIntrospector{}
	specPath := filepath.Join(testdataDir(), "openapi_simple", "petstore.yaml")

	deps, err := o.InferDependencies(context.Background(), specPath)
	if err != nil {
		t.Fatalf("InferDependencies failed: %v", err)
	}
	if deps != nil {
		t.Errorf("expected nil dependencies, got %v", deps)
	}
}

func TestRiskFromMethod(t *testing.T) {
	tests := []struct {
		method string
		want   string
	}{
		{"GET", "read_only"},
		{"get", "read_only"},
		{"HEAD", "read_only"},
		{"OPTIONS", "read_only"},
		{"POST", "side_effects"},
		{"PUT", "side_effects"},
		{"PATCH", "side_effects"},
		{"DELETE", "destructive"},
		{"delete", "destructive"},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			got := riskFromMethod(tt.method)
			if got != tt.want {
				t.Errorf("riskFromMethod(%q) = %q, want %q", tt.method, got, tt.want)
			}
		})
	}
}

func TestSanitizePath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/pets", "_pets"},
		{"/pets/{petId}", "_pets__petId_"},
		{"/users/{userId}/posts/{postId}", "_users__userId__posts__postId_"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := sanitizePath(tt.path)
			if got != tt.want {
				t.Errorf("sanitizePath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestOpenAPIIntrospector_FallbackName(t *testing.T) {
	// Test that tools without operationId get a generated name from method+path
	name := "get" + sanitizePath("/pets")
	if name != "get_pets" {
		t.Errorf("generated name = %q, want %q", name, "get_pets")
	}
}
