package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/foldermcp/foldermcp/internal/sandbox"
	"github.com/foldermcp/foldermcp/internal/state"
	"github.com/spf13/cobra"
)

var testToolCmd = &cobra.Command{
	Use:   "test <tool-name>",
	Short: "Test a tool by running it locally",
	Long: `Executes a single tool with the given JSON parameters and displays the
output, exit code, and duration. Useful for verifying tools before serving.`,
	Args: cobra.ExactArgs(1),
	RunE: runTestTool,
}

func init() {
	testToolCmd.Flags().String("params", "{}", "JSON parameters to pass to the tool")
	rootCmd.AddCommand(testToolCmd)
}

func runTestTool(cmd *cobra.Command, args []string) error {
	toolName := args[0]
	paramsStr, _ := cmd.Flags().GetString("params")

	dir, err := filepath.Abs(".")
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	// Validate params JSON.
	var params map[string]interface{}
	if err := json.Unmarshal([]byte(paramsStr), &params); err != nil {
		return fmt.Errorf("invalid --params JSON: %w", err)
	}

	// Open state store.
	stateDir := filepath.Join(dir, ".foldermcp")
	store, err := state.Open(stateDir)
	if err != nil {
		return fmt.Errorf("open state store: %w", err)
	}
	defer store.Close()

	// Get tool.
	tool, err := store.GetTool(toolName)
	if err != nil {
		return fmt.Errorf("get tool: %w", err)
	}
	if tool == nil {
		return fmt.Errorf("tool %q not found; run 'foldermcp init' first", toolName)
	}

	// Create executor.
	executor := sandbox.NewExecutor(sandbox.ExecutorConfig{
		TimeoutSeconds: 30,
		MaxOutputBytes: 100 * 1024,
	})

	// Create sanitizer.
	sanitizer := sandbox.NewSanitizer(100 * 1024)

	// Run tool.
	fmt.Fprintf(os.Stderr, "Testing tool: %s\n", tool.Name)
	fmt.Fprintf(os.Stderr, "Source: %s\n", tool.SourceFile)
	fmt.Fprintf(os.Stderr, "Params: %s\n", paramsStr)
	fmt.Fprintln(os.Stderr, "---")

	result, err := executor.RunPythonFile(
		context.Background(),
		tool.SourceFile,
		tool.Name,
		paramsStr,
		"", // venvPath
		nil,
	)
	if err != nil {
		return fmt.Errorf("execution failed: %w", err)
	}

	// Sanitize output.
	stdout := sanitizer.Sanitize(result.Stdout)
	stderr := sanitizer.Sanitize(result.Stderr)

	fmt.Fprintf(os.Stderr, "Exit code: %d\n", result.ExitCode)
	fmt.Fprintf(os.Stderr, "Duration:  %s\n", result.Duration)

	if stderr != "" {
		fmt.Fprintln(os.Stderr, "--- stderr ---")
		fmt.Fprint(os.Stderr, stderr)
		if stderr[len(stderr)-1] != '\n' {
			fmt.Fprintln(os.Stderr)
		}
	}

	fmt.Fprintln(os.Stderr, "--- stdout ---")
	fmt.Print(stdout)
	if len(stdout) > 0 && stdout[len(stdout)-1] != '\n' {
		fmt.Println()
	}

	return nil
}
