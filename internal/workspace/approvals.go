package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// SharedApprovals holds tool and resource approval states that are shared
// across team members via a YAML file in the project directory.
type SharedApprovals struct {
	Tools     map[string]ApprovalEntry `yaml:"tools"`
	Resources map[string]ApprovalEntry `yaml:"resources"`
}

// ApprovalEntry represents the approval state for a single tool or resource.
type ApprovalEntry struct {
	State       string `yaml:"state"`
	ApprovedBy  string `yaml:"approved_by,omitempty"`
	ApprovedAt  string `yaml:"approved_at,omitempty"`
	ContentHash string `yaml:"content_hash,omitempty"`
}

// NewSharedApprovals returns an initialized empty SharedApprovals.
func NewSharedApprovals() *SharedApprovals {
	return &SharedApprovals{
		Tools:     make(map[string]ApprovalEntry),
		Resources: make(map[string]ApprovalEntry),
	}
}

// LoadApprovals reads a SharedApprovals from the given YAML file path.
// If the file does not exist, it returns an empty SharedApprovals (not an error).
func LoadApprovals(path string) (*SharedApprovals, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewSharedApprovals(), nil
		}
		return nil, fmt.Errorf("read approvals: %w", err)
	}

	var a SharedApprovals
	if err := yaml.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("parse approvals: %w", err)
	}

	// Ensure maps are never nil.
	if a.Tools == nil {
		a.Tools = make(map[string]ApprovalEntry)
	}
	if a.Resources == nil {
		a.Resources = make(map[string]ApprovalEntry)
	}

	return &a, nil
}

// SaveApprovals writes the SharedApprovals to the given YAML file path using
// an atomic write pattern (temp file + rename) with an advisory lockfile.
func SaveApprovals(path string, a *SharedApprovals) error {
	// Ensure parent directory exists.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create approvals dir: %w", err)
	}

	// Advisory lockfile to coordinate concurrent writers.
	lockPath := path + ".lock"
	if err := acquireLock(lockPath); err != nil {
		return fmt.Errorf("acquire lock: %w", err)
	}
	defer releaseLock(lockPath)

	data, err := yaml.Marshal(a)
	if err != nil {
		return fmt.Errorf("marshal approvals: %w", err)
	}

	// Add a comment header.
	header := []byte("# FolderMCP Shared Approvals\n# This file is shared across team members on the same storage.\n")
	content := append(header, data...)

	// Atomic write: write to temp file, then rename.
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, content, 0o644); err != nil {
		return fmt.Errorf("write temp approvals: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		// Clean up temp file on rename failure.
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename approvals: %w", err)
	}

	return nil
}

// acquireLock creates an advisory lockfile. It retries briefly if the lock
// already exists, but gives up after a short timeout.
func acquireLock(lockPath string) error {
	for i := 0; i < 50; i++ {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_ = f.Close()
			return nil
		}
		if !os.IsExist(err) {
			return err
		}

		// Check if the lock is stale (older than 10 seconds).
		info, statErr := os.Stat(lockPath)
		if statErr == nil && time.Since(info.ModTime()) > 10*time.Second {
			_ = os.Remove(lockPath)
			continue
		}

		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for lock %s", lockPath)
}

// releaseLock removes the advisory lockfile.
func releaseLock(lockPath string) {
	_ = os.Remove(lockPath)
}
