// Package audit provides a structured JSON audit logger for tool invocations.
package audit

import (
	"encoding/json"
	"fmt"
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

// maxLogSize is the maximum audit log file size (10 MB) before rotation.
const maxLogSize = 10 * 1024 * 1024

// Logger writes structured JSON audit log entries to a file, one per line
// (JSONL format). It is safe for concurrent use.
type Logger struct {
	path string
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
	return &Logger{path: logPath, file: f}, nil
}

// Log writes a single JSON-line audit entry with a UTC timestamp.
// It performs size-based log rotation before writing.
func (l *Logger) Log(toolName, action, params, caller, resultStatus string) error {
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
		return fmt.Errorf("marshal audit entry: %w", err)
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Rotate if the log file exceeds the size limit.
	if err := l.rotateLocked(); err != nil {
		return fmt.Errorf("rotate audit log: %w", err)
	}

	if l.file != nil {
		if _, err := l.file.Write(append(data, '\n')); err != nil {
			return fmt.Errorf("write audit entry: %w", err)
		}
	}
	return nil
}

// rotateLocked performs size-based log rotation. When the log file exceeds
// maxLogSize, it is renamed to <path>.1 and a new file is opened.
// Must be called with l.mu held.
func (l *Logger) rotateLocked() error {
	if l.file == nil {
		return nil
	}
	info, err := l.file.Stat()
	if err != nil || info.Size() < maxLogSize {
		return nil
	}
	_ = l.file.Close()
	backupPath := l.path + ".1"
	_ = os.Remove(backupPath)
	if err := os.Rename(l.path, backupPath); err != nil {
		// Rename failed — try to reopen the original file and continue logging.
		f, reopenErr := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if reopenErr != nil {
			l.file = nil
			return fmt.Errorf("rotate rename failed: %w; reopen also failed: %v", err, reopenErr)
		}
		l.file = f
		return nil
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		l.file = nil
		return err
	}
	l.file = f
	return nil
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
