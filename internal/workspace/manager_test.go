package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspace_Open(t *testing.T) {
	// Create a temp directory to act as the project dir.
	projectDir := t.TempDir()

	ws, err := Open(projectDir)
	if err != nil {
		t.Fatalf("Open(%q) failed: %v", projectDir, err)
	}

	// Verify ProjectDir is set.
	if ws.ProjectDir == "" {
		t.Error("ProjectDir is empty")
	}

	// Verify LocalDir was created.
	info, err := os.Stat(ws.LocalDir)
	if err != nil {
		t.Fatalf("LocalDir %q does not exist: %v", ws.LocalDir, err)
	}
	if !info.IsDir() {
		t.Errorf("LocalDir %q is not a directory", ws.LocalDir)
	}

	// Verify LocalDir is under ~/.foldermcp/workspaces/
	home, _ := os.UserHomeDir()
	expectedPrefix := filepath.Join(home, ".foldermcp", "workspaces")
	rel, err := filepath.Rel(expectedPrefix, ws.LocalDir)
	if err != nil {
		t.Fatalf("LocalDir not under expected prefix: %v", err)
	}
	// rel should be a single directory name (the hash), not containing separators.
	if filepath.Dir(rel) != "." {
		t.Errorf("LocalDir hash directory unexpected: got rel=%q", rel)
	}

	// Verify path methods return expected locations.
	if ws.StateDBPath() != filepath.Join(ws.LocalDir, "state.db") {
		t.Errorf("StateDBPath() = %q, want %q", ws.StateDBPath(), filepath.Join(ws.LocalDir, "state.db"))
	}
	if ws.AuditLogPath() != filepath.Join(ws.LocalDir, "audit.log") {
		t.Errorf("AuditLogPath() = %q, want %q", ws.AuditLogPath(), filepath.Join(ws.LocalDir, "audit.log"))
	}
	if ws.VenvDir() != filepath.Join(ws.LocalDir, "venv") {
		t.Errorf("VenvDir() = %q, want %q", ws.VenvDir(), filepath.Join(ws.LocalDir, "venv"))
	}
	if ws.CacheDir() != filepath.Join(ws.LocalDir, "cache") {
		t.Errorf("CacheDir() = %q, want %q", ws.CacheDir(), filepath.Join(ws.LocalDir, "cache"))
	}
	if ws.APIKeyPath() != filepath.Join(ws.LocalDir, "api.key") {
		t.Errorf("APIKeyPath() = %q, want %q", ws.APIKeyPath(), filepath.Join(ws.LocalDir, "api.key"))
	}
	if ws.TLSDir() != filepath.Join(ws.LocalDir, "tls") {
		t.Errorf("TLSDir() = %q, want %q", ws.TLSDir(), filepath.Join(ws.LocalDir, "tls"))
	}
	if ws.ConfigPath() != filepath.Join(ws.ProjectDir, "foldermcp.yaml") {
		t.Errorf("ConfigPath() = %q, want %q", ws.ConfigPath(), filepath.Join(ws.ProjectDir, "foldermcp.yaml"))
	}
	if ws.ApprovalsPath() != filepath.Join(ws.ProjectDir, ".foldermcp", "approvals.yaml") {
		t.Errorf("ApprovalsPath() = %q, want %q", ws.ApprovalsPath(), filepath.Join(ws.ProjectDir, ".foldermcp", "approvals.yaml"))
	}

	// Clean up the workspace directory we created.
	t.Cleanup(func() { _ = os.RemoveAll(ws.LocalDir) })
}

func TestWorkspace_HashConsistency(t *testing.T) {
	// The same path must always produce the same hash.
	projectDir := t.TempDir()

	ws1, err := Open(projectDir)
	if err != nil {
		t.Fatalf("first Open failed: %v", err)
	}

	ws2, err := Open(projectDir)
	if err != nil {
		t.Fatalf("second Open failed: %v", err)
	}

	if ws1.LocalDir != ws2.LocalDir {
		t.Errorf("LocalDir mismatch: %q vs %q", ws1.LocalDir, ws2.LocalDir)
	}

	t.Cleanup(func() { _ = os.RemoveAll(ws1.LocalDir) })
}

func TestWorkspace_DifferentPaths(t *testing.T) {
	// Different paths must produce different hashes.
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	ws1, err := Open(dir1)
	if err != nil {
		t.Fatalf("Open(%q) failed: %v", dir1, err)
	}

	ws2, err := Open(dir2)
	if err != nil {
		t.Fatalf("Open(%q) failed: %v", dir2, err)
	}

	if ws1.LocalDir == ws2.LocalDir {
		t.Errorf("different paths produced the same LocalDir: %q", ws1.LocalDir)
	}

	t.Cleanup(func() {
		_ = os.RemoveAll(ws1.LocalDir)
		_ = os.RemoveAll(ws2.LocalDir)
	})
}

func TestComputeHash(t *testing.T) {
	h1 := computeHash("/some/path")
	h2 := computeHash("/some/path")
	h3 := computeHash("/other/path")

	if h1 != h2 {
		t.Errorf("same input produced different hashes: %q vs %q", h1, h2)
	}
	if h1 == h3 {
		t.Errorf("different inputs produced same hash: %q", h1)
	}
	if len(h1) != 12 {
		t.Errorf("hash length = %d, want 12", len(h1))
	}
}
