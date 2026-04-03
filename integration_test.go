//go:build integration

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// buildBinary compiles the foldermcp CLI to a temp directory and returns the
// absolute path to the resulting binary.
func buildBinary(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	binName := "foldermcp"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(dir, binName)

	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/foldermcp")
	cmd.Dir = projectRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build binary: %v\n%s", err, out)
	}

	return binPath
}

// projectRoot returns the absolute path to the project root (directory
// containing go.mod).
func projectRoot(t *testing.T) string {
	t.Helper()
	// The test file lives at the project root, so use its directory.
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine project root via runtime.Caller")
	}
	return filepath.Dir(filename)
}

// runCmd executes the binary with args, setting the working directory to dir.
// It returns the combined stdout+stderr output as a string.
func runCmd(t *testing.T, binary, dir string, args ...string) string {
	t.Helper()

	cmd := exec.Command(binary, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Some commands (like doctor) may return warnings but exit 0.
		// Only fail if it's a hard error we didn't expect.
		t.Logf("command %v exited with error: %v\noutput:\n%s", args, err, string(out))
	}
	return string(out)
}

// runCmdExpectSuccess is like runCmd but fails the test on a non-zero exit.
func runCmdExpectSuccess(t *testing.T, binary, dir string, args ...string) string {
	t.Helper()

	cmd := exec.Command(binary, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command %v failed: %v\noutput:\n%s", args, err, string(out))
	}
	return string(out)
}

// pythonAvailable returns true if a working Python interpreter is on PATH.
func pythonAvailable() bool {
	for _, name := range []string{"python3", "python"} {
		p, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		cmd := exec.Command(p, "--version")
		if err := cmd.Run(); err == nil {
			return true
		}
	}
	return false
}

func TestFullFlow_InitScanReviewCatalog(t *testing.T) {
	// Step 1: Skip if Python not available.
	if !pythonAvailable() {
		t.Skip("Python not available, skipping integration test")
	}

	// Step 2: Build the foldermcp binary.
	binary := buildBinary(t)
	t.Logf("binary: %s", binary)

	// Step 3: Create a temp directory with a simple Python test file.
	workDir := t.TempDir()
	pyFile := filepath.Join(workDir, "math_tools.py")
	pyContent := `def add(x: int, y: int) -> int:
    """Add two integers and return the sum."""
    return x + y

def multiply(a: float, b: float) -> float:
    """Multiply two numbers."""
    return a * b
`
	if err := os.WriteFile(pyFile, []byte(pyContent), 0644); err != nil {
		t.Fatalf("write python file: %v", err)
	}

	// Step 4: Run foldermcp init — verify output contains "Found".
	initOut := runCmdExpectSuccess(t, binary, workDir, "init", workDir)
	t.Logf("init output:\n%s", initOut)
	if !strings.Contains(initOut, "Found") {
		t.Errorf("init: expected output to contain 'Found', got:\n%s", initOut)
	}

	// Step 5: Run foldermcp catalog — verify output contains function name "add".
	catalogOut := runCmdExpectSuccess(t, binary, workDir, "catalog")
	t.Logf("catalog output:\n%s", catalogOut)
	if !strings.Contains(catalogOut, "add") {
		t.Errorf("catalog: expected output to contain 'add', got:\n%s", catalogOut)
	}

	// Step 6: Run foldermcp review --approve=add — verify output contains "Approved".
	reviewOut := runCmdExpectSuccess(t, binary, workDir, "review", "--approve=add")
	t.Logf("review output:\n%s", reviewOut)
	if !strings.Contains(reviewOut, "Approved") {
		t.Errorf("review: expected output to contain 'Approved', got:\n%s", reviewOut)
	}

	// Step 7: Run foldermcp status — verify output contains "enabled".
	statusOut := runCmdExpectSuccess(t, binary, workDir, "status")
	t.Logf("status output:\n%s", statusOut)
	if !strings.Contains(strings.ToLower(statusOut), "enabled") {
		t.Errorf("status: expected output to contain 'enabled', got:\n%s", statusOut)
	}

	// Step 8: Run foldermcp doctor — verify output contains "Python".
	doctorOut := runCmd(t, binary, workDir, "doctor")
	t.Logf("doctor output:\n%s", doctorOut)
	if !strings.Contains(doctorOut, "Python") {
		t.Errorf("doctor: expected output to contain 'Python', got:\n%s", doctorOut)
	}

	// Step 9: Run foldermcp test add --params='{"x": 1, "y": 2}' — verify result.
	testOut := runCmdExpectSuccess(t, binary, workDir, "test", "add", "--params", `{"x": 1, "y": 2}`)
	t.Logf("test output:\n%s", testOut)
	if !strings.Contains(testOut, "3") {
		t.Errorf("test: expected output to contain '3', got:\n%s", testOut)
	}
}
