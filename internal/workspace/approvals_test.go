package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSharedApprovals_SaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".foldermcp", "approvals.yaml")

	original := &SharedApprovals{
		Tools: map[string]ApprovalEntry{
			"query_db": {
				State:       "enabled",
				ApprovedBy:  "alice",
				ApprovedAt:  "2026-04-04T12:00:00Z",
				ContentHash: "sha256:abc123",
			},
			"delete_records": {
				State: "disabled",
			},
		},
		Resources: map[string]ApprovalEntry{
			"api-guide.pdf": {
				State: "enabled",
			},
		},
	}

	// Save.
	if err := SaveApprovals(path, original); err != nil {
		t.Fatalf("SaveApprovals failed: %v", err)
	}

	// Verify file exists.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("approvals file not created: %v", err)
	}

	// Load.
	loaded, err := LoadApprovals(path)
	if err != nil {
		t.Fatalf("LoadApprovals failed: %v", err)
	}

	// Verify tools.
	if len(loaded.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(loaded.Tools))
	}

	qdb := loaded.Tools["query_db"]
	if qdb.State != "enabled" {
		t.Errorf("query_db.State = %q, want %q", qdb.State, "enabled")
	}
	if qdb.ApprovedBy != "alice" {
		t.Errorf("query_db.ApprovedBy = %q, want %q", qdb.ApprovedBy, "alice")
	}
	if qdb.ContentHash != "sha256:abc123" {
		t.Errorf("query_db.ContentHash = %q, want %q", qdb.ContentHash, "sha256:abc123")
	}

	del := loaded.Tools["delete_records"]
	if del.State != "disabled" {
		t.Errorf("delete_records.State = %q, want %q", del.State, "disabled")
	}

	// Verify resources.
	if len(loaded.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(loaded.Resources))
	}
	pdf := loaded.Resources["api-guide.pdf"]
	if pdf.State != "enabled" {
		t.Errorf("api-guide.pdf.State = %q, want %q", pdf.State, "enabled")
	}
}

func TestSharedApprovals_LoadNonExistent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.yaml")

	a, err := LoadApprovals(path)
	if err != nil {
		t.Fatalf("LoadApprovals for non-existent file should not error: %v", err)
	}
	if a == nil {
		t.Fatal("LoadApprovals returned nil")
	}
	if len(a.Tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(a.Tools))
	}
	if len(a.Resources) != 0 {
		t.Errorf("expected 0 resources, got %d", len(a.Resources))
	}
}

func TestSharedApprovals_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "approvals.yaml")

	a := &SharedApprovals{
		Tools: map[string]ApprovalEntry{
			"test_tool": {State: "enabled"},
		},
		Resources: make(map[string]ApprovalEntry),
	}

	if err := SaveApprovals(path, a); err != nil {
		t.Fatalf("SaveApprovals failed: %v", err)
	}

	// Verify temp file was cleaned up (no .tmp file left behind).
	tmpPath := path + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf("temp file %q should not exist after save", tmpPath)
	}

	// Verify lock file was cleaned up.
	lockPath := path + ".lock"
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Errorf("lock file %q should not exist after save", lockPath)
	}

	// Verify the file has the expected header comment.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read approvals file: %v", err)
	}
	if !strings.HasPrefix(string(data), "# FolderMCP Shared Approvals") {
		t.Error("approvals file should start with comment header")
	}
}

func TestNewSharedApprovals(t *testing.T) {
	a := NewSharedApprovals()
	if a == nil {
		t.Fatal("NewSharedApprovals returned nil")
	}
	if a.Tools == nil {
		t.Error("Tools map is nil")
	}
	if a.Resources == nil {
		t.Error("Resources map is nil")
	}
}
