//go:build windows

package transport

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRandomPipeNamesDiffer ensures pipe names are unguessable (random) rather
// than a predictable derivation — the core of the squatting fix.
func TestRandomPipeNamesDiffer(t *testing.T) {
	a, err := randomPipeName()
	if err != nil {
		t.Fatal(err)
	}
	b, err := randomPipeName()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Errorf("pipe names not random: both %q", a)
	}
	if !strings.HasPrefix(a, `\\.\pipe\foldermcp-`) {
		t.Errorf("bad pipe name: %q", a)
	}
}

// TestListenWritesAndCleansEndpoint verifies the endpoint file is written with
// the pipe name and removed when the listener closes.
func TestListenWritesAndCleansEndpoint(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "serve.sock")
	l, err := Listen(sockPath)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	data, err := os.ReadFile(sockPath)
	if err != nil {
		t.Fatalf("endpoint file not written: %v", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(data)), `\\.\pipe\foldermcp-`) {
		t.Errorf("endpoint file contents = %q", string(data))
	}

	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
		t.Errorf("endpoint file not cleaned up after Close (err=%v)", err)
	}
}
