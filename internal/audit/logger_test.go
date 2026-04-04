package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogger_WritesJSONLine(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.jsonl")

	logger, err := NewLogger(logPath)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	defer logger.Close()

	if err := logger.Log("my_tool", "invoke", `{"x":1}`, "test-caller", "success"); err != nil {
		t.Fatalf("Log: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}

	line := strings.TrimSpace(string(data))
	var entry LogEntry
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		t.Fatalf("unmarshal JSON line: %v (line: %s)", err, line)
	}

	if entry.ToolName != "my_tool" {
		t.Errorf("ToolName = %q, want %q", entry.ToolName, "my_tool")
	}
	if entry.Action != "invoke" {
		t.Errorf("Action = %q, want %q", entry.Action, "invoke")
	}
	if entry.Params != `{"x":1}` {
		t.Errorf("Params = %q, want %q", entry.Params, `{"x":1}`)
	}
	if entry.Caller != "test-caller" {
		t.Errorf("Caller = %q, want %q", entry.Caller, "test-caller")
	}
	if entry.ResultStatus != "success" {
		t.Errorf("ResultStatus = %q, want %q", entry.ResultStatus, "success")
	}
	if entry.Timestamp == "" {
		t.Error("Timestamp should not be empty")
	}
}

func TestLogger_MultipleEntries(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.jsonl")

	logger, err := NewLogger(logPath)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	defer logger.Close()

	if err := logger.Log("tool_a", "invoke", "", "", "success"); err != nil {
		t.Fatalf("Log tool_a: %v", err)
	}
	if err := logger.Log("tool_b", "invoke", `{"key":"val"}`, "caller2", "error"); err != nil {
		t.Fatalf("Log tool_b: %v", err)
	}
	if err := logger.Log("tool_c", "invoke", "", "", "success"); err != nil {
		t.Fatalf("Log tool_c: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %v", len(lines), lines)
	}

	// Verify each line is valid JSON with the correct tool name.
	expectedTools := []string{"tool_a", "tool_b", "tool_c"}
	for i, line := range lines {
		var entry LogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Errorf("line %d: unmarshal error: %v", i, err)
			continue
		}
		if entry.ToolName != expectedTools[i] {
			t.Errorf("line %d: ToolName = %q, want %q", i, entry.ToolName, expectedTools[i])
		}
	}
}

func TestLogger_Close(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.jsonl")

	logger, err := NewLogger(logPath)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}

	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Calling Close again should not panic (file is nil).
	err = logger.Close()
	if err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
