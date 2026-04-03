// Package audit provides a structured JSON audit logger for tool invocations.
package audit

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// LogEntry represents a single audit log record written as a JSON line.
type LogEntry struct {
	Timestamp    string `json:"timestamp"`
	ToolName     string `json:"tool_name"`
	Action       string `json:"action"`
	Params       string `json:"params,omitempty"`
	Caller       string `json:"caller,omitempty"`
	ResultStatus string `json:"result_status"`
}

// Logger writes structured JSON audit log entries to a file, one per line
// (JSONL format). It is safe for concurrent use.
type Logger struct {
	file *os.File
	mu   sync.Mutex
}

// NewLogger opens (or creates) the log file at logPath in append mode and
// returns a ready-to-use Logger.
func NewLogger(logPath string) (*Logger, error) {
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &Logger{file: f}, nil
}

// Log writes a single JSON-line audit entry with a UTC timestamp.
func (l *Logger) Log(toolName, action, params, caller, resultStatus string) {
	entry := LogEntry{
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		ToolName:     toolName,
		Action:       action,
		Params:       params,
		Caller:       caller,
		ResultStatus: resultStatus,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return // best-effort: don't crash on marshal failure
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file != nil {
		_, _ = l.file.Write(append(data, '\n'))
	}
}

// Close closes the underlying log file.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}
