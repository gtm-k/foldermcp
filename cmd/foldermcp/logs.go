package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/foldermcp/foldermcp/internal/audit"
	"github.com/foldermcp/foldermcp/internal/workspace"
	"github.com/spf13/cobra"
)

var logsFollow bool

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "View the audit log",
	RunE:  runLogs,
}

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "Follow the log (like tail -f)")
	rootCmd.AddCommand(logsCmd)
}

func runLogs(cmd *cobra.Command, args []string) error {
	dir, err := filepath.Abs(".")
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	ws, err := workspace.Open(dir)
	if err != nil {
		return fmt.Errorf("open workspace: %w", err)
	}

	logPath := ws.AuditLogPath()

	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, "No audit log found. Run some tools first.")
		return nil
	}

	if logsFollow {
		return followLog(logPath)
	}
	return tailLog(logPath, 50)
}

// tailLog prints the last n lines of the log file, formatted.
func tailLog(logPath string, n int) error {
	f, err := os.Open(logPath)
	if err != nil {
		return fmt.Errorf("open audit log: %w", err)
	}
	defer func() { _ = f.Close() }()

	// Read all lines into a ring buffer of size n.
	scanner := bufio.NewScanner(f)
	lines := make([]string, 0, n)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > n {
			lines = lines[1:]
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read audit log: %w", err)
	}

	for _, line := range lines {
		fmt.Println(formatLogLine(line))
	}
	return nil
}

// followLog tails the file, printing new lines as they appear.
func followLog(logPath string) error {
	f, err := os.Open(logPath)
	if err != nil {
		return fmt.Errorf("open audit log: %w", err)
	}
	defer func() { _ = f.Close() }()

	// Seek to end of file.
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return fmt.Errorf("seek audit log: %w", err)
	}

	reader := bufio.NewReader(f)

	fmt.Fprintln(os.Stderr, "Following audit log (Ctrl+C to stop)...")

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				return fmt.Errorf("read audit log: %w", err)
			}
			// No new data yet — poll every 500ms.
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if line != "" {
			fmt.Print(formatLogLine(line))
			if line[len(line)-1] != '\n' {
				fmt.Println()
			}
		}
	}
}

// formatLogLine parses a JSON log line and returns a formatted string.
func formatLogLine(line string) string {
	var entry audit.LogEntry
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		// If it's not valid JSON, return as-is.
		return line
	}

	return fmt.Sprintf("[%s] %s %s %s",
		entry.Timestamp,
		entry.ToolName,
		entry.Action,
		entry.ResultStatus,
	)
}
