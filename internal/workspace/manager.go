package workspace

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

// Workspace represents the split-storage layout for a FolderMCP project.
// ProjectDir is the scanned directory (may be on NAS / network share).
// LocalDir is always on the local disk (~/.foldermcp/workspaces/<hash>/).
type Workspace struct {
	ProjectDir  string // the scanned directory (may be on NAS)
	LocalDir    string // ~/.foldermcp/workspaces/<hash>/ (always local)
	IsNetworkFS bool   // detected at runtime
}

// Open creates a Workspace for the given project directory.
// It canonicalizes the path, computes a deterministic local directory hash,
// creates the local directory if needed, and detects whether the project
// directory resides on a network filesystem.
func Open(projectDir string) (*Workspace, error) {
	// 1. Canonicalize projectDir: resolve symlinks and normalize the path.
	canonical, err := filepath.EvalSymlinks(projectDir)
	if err != nil {
		// If EvalSymlinks fails (e.g. path doesn't exist yet), fall back to Abs.
		canonical, err = filepath.Abs(projectDir)
		if err != nil {
			return nil, fmt.Errorf("canonicalize project dir: %w", err)
		}
	}
	canonical = filepath.Clean(canonical)

	// 2. Compute workspace hash: first 12 hex chars of SHA-256 of canonical path.
	hash := computeHash(canonical)

	// 3. LocalDir = ~/.foldermcp/workspaces/<hash>/
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("determine home directory: %w", err)
	}
	localDir := filepath.Join(home, ".foldermcp", "workspaces", hash)

	// 4. Create LocalDir if needed.
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		return nil, fmt.Errorf("create local workspace dir: %w", err)
	}

	// 5. Detect if projectDir is on a network filesystem.
	isNetwork := detectNetworkFS(canonical)

	return &Workspace{
		ProjectDir:  canonical,
		LocalDir:    localDir,
		IsNetworkFS: isNetwork,
	}, nil
}

// computeHash returns the first 12 hex characters of the SHA-256 digest of s.
func computeHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h[:])[:12]
}

// StateDBPath returns the path to the local SQLite state database.
func (w *Workspace) StateDBPath() string {
	return filepath.Join(w.LocalDir, "state.db")
}

// AuditLogPath returns the path to the local audit log file.
func (w *Workspace) AuditLogPath() string {
	return filepath.Join(w.LocalDir, "audit.log")
}

// VenvDir returns the path to the local Python virtual environment directory.
func (w *Workspace) VenvDir() string {
	return filepath.Join(w.LocalDir, "venv")
}

// CacheDir returns the path to the local cache directory.
func (w *Workspace) CacheDir() string {
	return filepath.Join(w.LocalDir, "cache")
}

// APIKeyPath returns the path to the local API key file.
func (w *Workspace) APIKeyPath() string {
	return filepath.Join(w.LocalDir, "api.key")
}

// TLSDir returns the path to the local TLS certificate directory.
func (w *Workspace) TLSDir() string {
	return filepath.Join(w.LocalDir, "tls")
}

// ConfigPath returns the path to the project's foldermcp.yaml configuration.
// This file lives in the project directory (shared on NAS).
func (w *Workspace) ConfigPath() string {
	return filepath.Join(w.ProjectDir, "foldermcp.yaml")
}

// ApprovalsPath returns the path to the shared approvals file.
// This file lives in ProjectDir/.foldermcp/ so it is shared on NAS.
func (w *Workspace) ApprovalsPath() string {
	return filepath.Join(w.ProjectDir, ".foldermcp", "approvals.yaml")
}
