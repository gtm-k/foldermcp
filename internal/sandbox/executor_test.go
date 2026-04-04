package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecutor_RunPythonExpression(t *testing.T) {
	e := NewExecutor(ExecutorConfig{})

	result, err := e.RunPython(context.Background(), "print(2 + 2)", "", nil)
	if err != nil {
		t.Fatalf("RunPython() error: %v", err)
	}

	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d (stderr: %s)", result.ExitCode, result.Stderr)
	}
	if strings.TrimSpace(result.Stdout) != "4" {
		t.Errorf("expected stdout '4', got %q", result.Stdout)
	}
	if result.Duration <= 0 {
		t.Error("expected positive duration")
	}
}

func TestExecutor_Timeout(t *testing.T) {
	e := NewExecutor(ExecutorConfig{
		TimeoutSeconds: 1,
	})

	_, err := e.RunPython(context.Background(), "import time; time.sleep(10)", "", nil)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timeout") && !strings.Contains(err.Error(), "deadline") && !strings.Contains(err.Error(), "killed") {
		t.Errorf("expected timeout-related error, got: %v", err)
	}
}

func TestExecutor_RunPythonFile(t *testing.T) {
	// Create a temp Python file with a simple function
	dir := t.TempDir()
	pyFile := filepath.Join(dir, "tools.py")

	code := `def greet(name, greeting="Hello"):
    return f"{greeting}, {name}!"
`
	if err := os.WriteFile(pyFile, []byte(code), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	e := NewExecutor(ExecutorConfig{})

	result, err := e.RunPythonFile(
		context.Background(),
		pyFile,
		"greet",
		`{"name": "World", "greeting": "Hi"}`,
		"",
		nil,
	)
	if err != nil {
		t.Fatalf("RunPythonFile() error: %v", err)
	}

	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d (stderr: %s)", result.ExitCode, result.Stderr)
	}

	stdout := strings.TrimSpace(result.Stdout)
	// The output should be JSON-encoded: "Hi, World!"
	if stdout != `"Hi, World!"` {
		t.Errorf("expected '\"Hi, World!\"', got %q", stdout)
	}
}

func TestExecutor_RunPythonFileNullResult(t *testing.T) {
	// Test function that returns None
	dir := t.TempDir()
	pyFile := filepath.Join(dir, "tools.py")

	code := `def do_nothing():
    pass
`
	if err := os.WriteFile(pyFile, []byte(code), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	e := NewExecutor(ExecutorConfig{})

	result, err := e.RunPythonFile(
		context.Background(),
		pyFile,
		"do_nothing",
		`{}`,
		"",
		nil,
	)
	if err != nil {
		t.Fatalf("RunPythonFile() error: %v", err)
	}

	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d (stderr: %s)", result.ExitCode, result.Stderr)
	}

	stdout := strings.TrimSpace(result.Stdout)
	if stdout != "null" {
		t.Errorf("expected 'null', got %q", stdout)
	}
}

func TestExecutor_EnvVars(t *testing.T) {
	e := NewExecutor(ExecutorConfig{})

	env := map[string]string{
		"MY_TEST_VAR": "hello_from_test",
	}

	result, err := e.RunPython(context.Background(), "import os; print(os.environ.get('MY_TEST_VAR', ''))", "", env)
	if err != nil {
		t.Fatalf("RunPython() error: %v", err)
	}

	if strings.TrimSpace(result.Stdout) != "hello_from_test" {
		t.Errorf("expected 'hello_from_test', got %q", result.Stdout)
	}
}

func TestExecutor_SyntaxError(t *testing.T) {
	e := NewExecutor(ExecutorConfig{})

	result, err := e.RunPython(context.Background(), "this is not valid python!!!", "", nil)
	if err != nil {
		t.Fatalf("RunPython() should not return error for non-zero exit, got: %v", err)
	}

	if result.ExitCode == 0 {
		t.Error("expected non-zero exit code for syntax error")
	}
	if result.Stderr == "" {
		t.Error("expected stderr output for syntax error")
	}
}

func TestValidatePath_InsideWorkspace(t *testing.T) {
	dir := t.TempDir()
	inner := filepath.Join(dir, "subdir")
	if err := os.MkdirAll(inner, 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(inner, "tool.py")
	if err := os.WriteFile(file, []byte("pass"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := validatePath(file, dir); err != nil {
		t.Errorf("expected no error for path inside workspace, got: %v", err)
	}
}

func TestValidatePath_OutsideWorkspace(t *testing.T) {
	workspaceDir := t.TempDir()
	outsideDir := t.TempDir()
	file := filepath.Join(outsideDir, "evil.py")
	if err := os.WriteFile(file, []byte("pass"), 0644); err != nil {
		t.Fatal(err)
	}

	err := validatePath(file, workspaceDir)
	if err == nil {
		t.Fatal("expected error for path outside workspace, got nil")
	}
	if !strings.Contains(err.Error(), "outside workspace") {
		t.Errorf("expected 'outside workspace' in error, got: %v", err)
	}
}

func TestValidatePath_WorkspaceRootItself(t *testing.T) {
	dir := t.TempDir()
	// The workspace root itself should be accepted.
	if err := validatePath(dir, dir); err != nil {
		t.Errorf("expected no error for workspace root itself, got: %v", err)
	}
}

func TestRunPythonFile_PathTraversalBlocked(t *testing.T) {
	workspaceDir := t.TempDir()
	outsideDir := t.TempDir()

	// Create a Python file outside the workspace.
	outsideFile := filepath.Join(outsideDir, "evil.py")
	if err := os.WriteFile(outsideFile, []byte("def evil(): return 'pwned'"), 0644); err != nil {
		t.Fatal(err)
	}

	e := NewExecutor(ExecutorConfig{
		WorkspaceRoot: workspaceDir,
	})

	_, err := e.RunPythonFile(context.Background(), outsideFile, "evil", "{}", "", nil)
	if err == nil {
		t.Fatal("expected path validation error, got nil")
	}
	if !strings.Contains(err.Error(), "path validation failed") {
		t.Errorf("expected 'path validation failed' in error, got: %v", err)
	}
}

func TestRunPythonFile_NoWorkspaceRoot_Allowed(t *testing.T) {
	// When WorkspaceRoot is empty, path validation is skipped.
	dir := t.TempDir()
	pyFile := filepath.Join(dir, "tool.py")
	if err := os.WriteFile(pyFile, []byte("def hello(): return 'hi'"), 0644); err != nil {
		t.Fatal(err)
	}

	e := NewExecutor(ExecutorConfig{})

	result, err := e.RunPythonFile(context.Background(), pyFile, "hello", "{}", "", nil)
	if err != nil {
		t.Fatalf("expected no error when WorkspaceRoot is empty, got: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d (stderr: %s)", result.ExitCode, result.Stderr)
	}
}

func TestSanitizer_TruncatesOutput(t *testing.T) {
	s := NewSanitizer(100)

	// Use a string with spaces so it won't be caught by the longTokenPattern
	// (which matches 40+ contiguous alphanumeric chars).
	input := strings.Repeat("hello world ", 20) // 240 chars
	output := s.Sanitize(input)

	if len(output) > 200 { // truncated + message should be bounded
		t.Errorf("expected truncated output, got length %d", len(output))
	}
	if !strings.Contains(output, "[output truncated at 100 bytes]") {
		t.Errorf("expected truncation message, got: %q", output)
	}
}

func TestSanitizer_RedactsSecrets(t *testing.T) {
	s := NewSanitizer(0) // no truncation limit

	input := "My AWS key is AKIAIOSFODNN7EXAMPLE and that's it"
	output := s.Sanitize(input)

	if strings.Contains(output, "AKIAIOSFODNN7EXAMPLE") {
		t.Errorf("expected AWS key to be redacted, got: %q", output)
	}
	if !strings.Contains(output, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in output, got: %q", output)
	}
}

func TestSanitizer_RedactsGitHubPAT(t *testing.T) {
	s := NewSanitizer(0)

	input := "token: ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij"
	output := s.Sanitize(input)

	if strings.Contains(output, "ghp_") {
		t.Errorf("expected GitHub PAT to be redacted, got: %q", output)
	}
	if !strings.Contains(output, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in output, got: %q", output)
	}
}

func TestSanitizer_RedactsOpenAIKey(t *testing.T) {
	s := NewSanitizer(0)

	input := "key=sk-proj-abcdefghijklmnopqrstuvwxyz1234567890ABCDEF"
	output := s.Sanitize(input)

	if strings.Contains(output, "sk-proj-") {
		t.Errorf("expected OpenAI key to be redacted, got: %q", output)
	}
}

func TestSanitizer_RedactsPrivateKeyHeader(t *testing.T) {
	s := NewSanitizer(0)

	input := "-----BEGIN RSA PRIVATE KEY----- some content here"
	output := s.Sanitize(input)

	if strings.Contains(output, "BEGIN RSA PRIVATE KEY") {
		t.Errorf("expected private key header to be redacted, got: %q", output)
	}
}

func TestSanitizer_PassesThroughCleanOutput(t *testing.T) {
	s := NewSanitizer(0)

	input := "This is perfectly normal output\nWith multiple lines\nAnd numbers 12345"
	output := s.Sanitize(input)

	if output != input {
		t.Errorf("expected clean output to pass through unchanged\ngot:  %q\nwant: %q", output, input)
	}
}

func TestSanitizer_SanitizeParams(t *testing.T) {
	s := NewSanitizer(0)

	// Test that long token-like strings are redacted
	longToken := strings.Repeat("a", 50) // 50 chars of alphanumeric
	input := "param=" + longToken
	output := s.SanitizeParams(input)

	if strings.Contains(output, longToken) {
		t.Errorf("expected long token to be redacted in params, got: %q", output)
	}

	// Test that secrets are also redacted in params
	input2 := "key=AKIAIOSFODNN7EXAMPLE"
	output2 := s.SanitizeParams(input2)

	if strings.Contains(output2, "AKIAIOSFODNN7EXAMPLE") {
		t.Errorf("expected AWS key to be redacted in params, got: %q", output2)
	}
}

func TestSanitizer_SanitizeParamsShortStringsPass(t *testing.T) {
	s := NewSanitizer(0)

	input := `{"name": "Alice", "age": 30}`
	output := s.SanitizeParams(input)

	if output != input {
		t.Errorf("expected short clean params to pass through\ngot:  %q\nwant: %q", output, input)
	}
}
