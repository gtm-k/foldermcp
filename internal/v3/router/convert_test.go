package router

import (
	"strings"
	"testing"
)

func TestSanitizeSourceError(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantSafe bool // result must NOT contain the raw input
	}{
		{"empty", "", true},
		{"sql_leak", "near \"SELECT\": syntax error at offset 42", true},
		{"sqlite_leak", "sqlite3: database is locked", true},
		{"embed_leak", "embedder: model qwen2.5 failed to load", true},
		{"timeout", "context deadline exceeded", true},
		{"connection", "dial tcp 127.0.0.1:50051: connection refused", true},
		{"generic", "something went wrong internally", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeSourceError(tt.input)
			if tt.input == "" {
				if got != "" {
					t.Errorf("empty input should yield empty output, got %q", got)
				}
				return
			}
			if got == tt.input {
				t.Errorf("sanitizeSourceError leaked raw error: %q", got)
			}
			if strings.Contains(got, "SELECT") || strings.Contains(got, "sqlite3") || strings.Contains(got, "qwen") {
				t.Errorf("sanitized message still contains internal details: %q", got)
			}
		})
	}
}
