package state

import (
	"os"
	"testing"
)

func setupTestStore(t *testing.T) *Store {
	t.Helper()
	dir, err := os.MkdirTemp("", "foldermcp-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open(%q) failed: %v", dir, err)
	}
	t.Cleanup(func() { store.Close() })

	return store
}

func TestStore_CreateAndGetTool(t *testing.T) {
	store := setupTestStore(t)

	tool := Tool{
		Name:        "greet",
		SourceFile:  "tools/greet.py",
		Description: "Greets a user",
		InputSchema: `{"type":"object","properties":{"name":{"type":"string"}}}`,
		Risk:        "low",
	}

	// Upsert the tool
	if err := store.UpsertTool(tool); err != nil {
		t.Fatalf("UpsertTool failed: %v", err)
	}

	// Get the tool back
	got, err := store.GetTool("greet")
	if err != nil {
		t.Fatalf("GetTool failed: %v", err)
	}

	// Verify fields
	if got.Name != tool.Name {
		t.Errorf("Name = %q, want %q", got.Name, tool.Name)
	}
	if got.SourceFile != tool.SourceFile {
		t.Errorf("SourceFile = %q, want %q", got.SourceFile, tool.SourceFile)
	}
	if got.Description != tool.Description {
		t.Errorf("Description = %q, want %q", got.Description, tool.Description)
	}
	if got.InputSchema != tool.InputSchema {
		t.Errorf("InputSchema = %q, want %q", got.InputSchema, tool.InputSchema)
	}
	if got.Risk != tool.Risk {
		t.Errorf("Risk = %q, want %q", got.Risk, tool.Risk)
	}
	// Defaults
	if got.State != "pending" {
		t.Errorf("State = %q, want %q", got.State, "pending")
	}
	if got.DepState != "resolved" {
		t.Errorf("DepState = %q, want %q", got.DepState, "resolved")
	}

	// Upsert again with changed description — state should be preserved
	tool.Description = "Greets a user by name"
	if err := store.UpsertTool(tool); err != nil {
		t.Fatalf("UpsertTool (update) failed: %v", err)
	}

	// First update the state to something other than default
	if err := store.UpdateToolState("greet", "enabled"); err != nil {
		t.Fatalf("UpdateToolState failed: %v", err)
	}

	// Upsert again — state should be preserved as "enabled"
	tool.Description = "Greets a user warmly"
	if err := store.UpsertTool(tool); err != nil {
		t.Fatalf("UpsertTool (update with state) failed: %v", err)
	}

	got, err = store.GetTool("greet")
	if err != nil {
		t.Fatalf("GetTool after re-upsert failed: %v", err)
	}
	if got.Description != "Greets a user warmly" {
		t.Errorf("Description after upsert = %q, want %q", got.Description, "Greets a user warmly")
	}
	if got.State != "enabled" {
		t.Errorf("State after re-upsert = %q, want %q (should be preserved)", got.State, "enabled")
	}
}

func TestStore_GetTool_NotFound(t *testing.T) {
	store := setupTestStore(t)

	got, err := store.GetTool("nonexistent")
	if err != nil {
		t.Fatalf("GetTool should not error for missing tool, got: %v", err)
	}
	if got != nil {
		t.Errorf("GetTool for missing tool should return nil, got: %+v", got)
	}
}

func TestStore_ListTools(t *testing.T) {
	store := setupTestStore(t)

	tools := []Tool{
		{Name: "beta_tool", SourceFile: "tools/beta.py", Description: "Beta", InputSchema: "{}", Risk: "low"},
		{Name: "alpha_tool", SourceFile: "tools/alpha.py", Description: "Alpha", InputSchema: "{}", Risk: "medium"},
	}

	for _, tool := range tools {
		if err := store.UpsertTool(tool); err != nil {
			t.Fatalf("UpsertTool(%q) failed: %v", tool.Name, err)
		}
	}

	list, err := store.ListTools()
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}

	if len(list) != 2 {
		t.Fatalf("ListTools returned %d tools, want 2", len(list))
	}

	// Should be ordered by name
	if list[0].Name != "alpha_tool" {
		t.Errorf("list[0].Name = %q, want %q", list[0].Name, "alpha_tool")
	}
	if list[1].Name != "beta_tool" {
		t.Errorf("list[1].Name = %q, want %q", list[1].Name, "beta_tool")
	}
}

func TestStore_UpdateToolState(t *testing.T) {
	store := setupTestStore(t)

	tool := Tool{
		Name:        "calculator",
		SourceFile:  "tools/calc.py",
		Description: "Does math",
		InputSchema: `{"type":"object"}`,
		Risk:        "low",
	}

	if err := store.UpsertTool(tool); err != nil {
		t.Fatalf("UpsertTool failed: %v", err)
	}

	// Verify initial state is pending
	got, err := store.GetTool("calculator")
	if err != nil {
		t.Fatalf("GetTool failed: %v", err)
	}
	if got.State != "pending" {
		t.Errorf("initial State = %q, want %q", got.State, "pending")
	}

	// Update to enabled
	if err := store.UpdateToolState("calculator", "enabled"); err != nil {
		t.Fatalf("UpdateToolState failed: %v", err)
	}

	got, err = store.GetTool("calculator")
	if err != nil {
		t.Fatalf("GetTool after state update failed: %v", err)
	}
	if got.State != "enabled" {
		t.Errorf("State after update = %q, want %q", got.State, "enabled")
	}

	// Invalid state should return error
	if err := store.UpdateToolState("calculator", "bogus"); err == nil {
		t.Error("expected error for invalid tool state, got nil")
	}

	// Non-existent tool should return error
	if err := store.UpdateToolState("nonexistent", "enabled"); err == nil {
		t.Error("expected error for nonexistent tool, got nil")
	}
}

func TestStore_UpdateDepState(t *testing.T) {
	store := setupTestStore(t)

	tool := Tool{
		Name:        "fetcher",
		SourceFile:  "tools/fetch.py",
		Description: "Fetches data",
		InputSchema: `{}`,
		Risk:        "medium",
	}

	if err := store.UpsertTool(tool); err != nil {
		t.Fatalf("UpsertTool failed: %v", err)
	}

	// Verify initial dep_state is resolved (default).
	got, _ := store.GetTool("fetcher")
	if got.DepState != "resolved" {
		t.Errorf("initial DepState = %q, want %q", got.DepState, "resolved")
	}

	// Update to resolved
	if err := store.UpdateDepState("fetcher", "resolved"); err != nil {
		t.Fatalf("UpdateDepState failed: %v", err)
	}

	got, _ = store.GetTool("fetcher")
	if got.DepState != "resolved" {
		t.Errorf("DepState after update = %q, want %q", got.DepState, "resolved")
	}

	// Invalid dep state should return error
	if err := store.UpdateDepState("fetcher", "bogus"); err == nil {
		t.Error("expected error for invalid dep state, got nil")
	}

	// Non-existent tool should return error
	if err := store.UpdateDepState("nonexistent", "resolved"); err == nil {
		t.Error("expected error for nonexistent tool, got nil")
	}
}

func TestStore_LogAudit(t *testing.T) {
	store := setupTestStore(t)

	err := store.LogAudit("greet", "invoke", `{"name":"Alice"}`, "test-caller", "success")
	if err != nil {
		t.Fatalf("LogAudit failed: %v", err)
	}

	// Log a second entry to verify multiple inserts work
	err = store.LogAudit("greet", "invoke", `{"name":"Bob"}`, "test-caller", "error")
	if err != nil {
		t.Fatalf("LogAudit (second) failed: %v", err)
	}
}

func TestStore_OpenCreatesDirectory(t *testing.T) {
	dir, err := os.MkdirTemp("", "foldermcp-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	stateDir := dir + "/subdir"
	store, err := Open(stateDir)
	if err != nil {
		t.Fatalf("Open(%q) failed: %v", stateDir, err)
	}
	defer store.Close()

	// The .foldermcp directory should have been created
	info, err := os.Stat(stateDir)
	if err != nil {
		t.Fatalf("stateDir not created: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("stateDir is not a directory")
	}
}
