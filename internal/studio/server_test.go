package studio

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/foldermcp/foldermcp/internal/state"
)

// setupTestServer creates a temporary state directory with a seeded database
// and returns a ready StudioServer plus a cleanup function.
func setupTestServer(t *testing.T) (*StudioServer, func()) {
	t.Helper()

	tmpDir := t.TempDir()
	stateDir := filepath.Join(tmpDir, ".foldermcp")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("create state dir: %v", err)
	}

	// Seed the database using the state store.
	store, err := state.Open(stateDir)
	if err != nil {
		t.Fatalf("open state store: %v", err)
	}

	tools := []state.Tool{
		{Name: "hello_world", SourceFile: "scripts/hello.py", Description: "Says hello", Risk: "low", State: "enabled", DepState: "resolved"},
		{Name: "deploy_prod", SourceFile: "scripts/deploy.py", Description: "Deploy to production", Risk: "high", State: "pending", DepState: "resolved"},
		{Name: "clean_cache", SourceFile: "scripts/cache.py", Description: "Clear cache", Risk: "medium", State: "disabled", DepState: "failed"},
	}
	for _, tool := range tools {
		if err := store.UpsertTool(tool); err != nil {
			t.Fatalf("upsert tool %q: %v", tool.Name, err)
		}
	}

	// Seed audit log entries.
	if err := store.LogAudit("hello_world", "invoke", "{}", "claude", "success"); err != nil {
		t.Fatalf("log audit: %v", err)
	}
	if err := store.LogAudit("deploy_prod", "invoke", "{}", "claude", "error"); err != nil {
		t.Fatalf("log audit: %v", err)
	}

	_ = store.Close()

	srv := NewStudioServer(stateDir, 0)
	cleanup := func() {
		_ = srv.Close()
	}
	return srv, cleanup
}

func TestStudioServer_ToolsEndpoint(t *testing.T) {
	srv, cleanup := setupTestServer(t)
	defer cleanup()

	handler, err := srv.Handler()
	if err != nil {
		t.Fatalf("get handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tools", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("expected content-type application/json, got %q", ct)
	}

	var tools []toolJSON
	if err := json.NewDecoder(resp.Body).Decode(&tools); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(tools))
	}

	// Tools are sorted by name.
	expected := []string{"clean_cache", "deploy_prod", "hello_world"}
	for i, name := range expected {
		if tools[i].Name != name {
			t.Errorf("tool[%d]: expected name %q, got %q", i, name, tools[i].Name)
		}
	}

	// Verify fields on first tool.
	if tools[0].State != "disabled" {
		t.Errorf("clean_cache: expected state 'disabled', got %q", tools[0].State)
	}
	if tools[0].Risk != "medium" {
		t.Errorf("clean_cache: expected risk 'medium', got %q", tools[0].Risk)
	}
}

func TestStudioServer_StatusEndpoint(t *testing.T) {
	srv, cleanup := setupTestServer(t)
	defer cleanup()

	handler, err := srv.Handler()
	if err != nil {
		t.Fatalf("get handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("expected content-type application/json, got %q", ct)
	}

	var status statusJSON
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if status.TotalTools != 3 {
		t.Errorf("expected total_tools=3, got %d", status.TotalTools)
	}

	if status.StateCounts["enabled"] != 1 {
		t.Errorf("expected 1 enabled, got %d", status.StateCounts["enabled"])
	}
	if status.StateCounts["pending"] != 1 {
		t.Errorf("expected 1 pending, got %d", status.StateCounts["pending"])
	}
	if status.StateCounts["disabled"] != 1 {
		t.Errorf("expected 1 disabled, got %d", status.StateCounts["disabled"])
	}

	if status.DepCounts["resolved"] != 2 {
		t.Errorf("expected 2 resolved deps, got %d", status.DepCounts["resolved"])
	}
	if status.DepCounts["failed"] != 1 {
		t.Errorf("expected 1 failed dep, got %d", status.DepCounts["failed"])
	}

	if status.UptimeString == "" {
		t.Error("expected non-empty uptime string")
	}
}

func TestStudioServer_AuditEndpoint(t *testing.T) {
	srv, cleanup := setupTestServer(t)
	defer cleanup()

	handler, err := srv.Handler()
	if err != nil {
		t.Fatalf("get handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/audit", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var entries []auditJSON
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 audit entries, got %d", len(entries))
	}

	// Entries are ordered by id DESC, so most recent first.
	if entries[0].ToolName != "deploy_prod" {
		t.Errorf("expected first entry tool_name 'deploy_prod', got %q", entries[0].ToolName)
	}
	if entries[0].ResultStatus != "error" {
		t.Errorf("expected first entry result_status 'error', got %q", entries[0].ResultStatus)
	}
	if entries[1].ToolName != "hello_world" {
		t.Errorf("expected second entry tool_name 'hello_world', got %q", entries[1].ToolName)
	}
}

func TestStudioServer_IndexHTML(t *testing.T) {
	srv, cleanup := setupTestServer(t)
	defer cleanup()

	handler, err := srv.Handler()
	if err != nil {
		t.Fatalf("get handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	body := w.Body.String()
	if len(body) < 100 {
		t.Error("expected HTML body with content, got very short response")
	}
}
