package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/foldermcp/foldermcp/internal/audit"
	"github.com/foldermcp/foldermcp/internal/sandbox"
	"github.com/foldermcp/foldermcp/internal/state"
	"github.com/foldermcp/foldermcp/internal/workspace"
	"github.com/spf13/cobra"
)

var testToolCmd = &cobra.Command{
	Use:   "test [tool-name]",
	Short: "Test a tool by running it locally",
	Long: `Executes a single tool with the given JSON parameters and displays the
output, exit code, and duration. Useful for verifying tools before serving.
Use --all to smoke-test all enabled tools with empty params.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runTestTool,
}

var testForce bool
var testAll bool

func init() {
	testToolCmd.Flags().String("params", "{}", "JSON parameters to pass to the tool")
	testToolCmd.Flags().BoolVar(&testForce, "force", false, "Execute even if tool is pending/disabled (bypass safety check)")
	testToolCmd.Flags().BoolVar(&testAll, "all", false, "Smoke-test all enabled tools with empty params")
	rootCmd.AddCommand(testToolCmd)
}

func runTestTool(cmd *cobra.Command, args []string) error {
	// Require either --all or a tool name.
	if !testAll && len(args) == 0 {
		return fmt.Errorf("provide a <tool-name> or use --all")
	}

	dir, err := filepath.Abs(".")
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	ws, err := workspace.Open(dir)
	if err != nil {
		return fmt.Errorf("open workspace: %w", err)
	}

	// Open state store (SQLite always on local disk).
	store, err := state.Open(ws.LocalDir)
	if err != nil {
		return fmt.Errorf("open state store: %w", err)
	}
	defer func() { _ = store.Close() }()

	// Create audit logger.
	logger, err := audit.NewLogger(ws.AuditLogPath())
	if err == nil {
		defer func() { _ = logger.Close() }()
	}

	// Create executor with workspace root for path traversal validation.
	executor := sandbox.NewExecutor(sandbox.ExecutorConfig{
		TimeoutSeconds: 30,
		MaxOutputBytes: 100 * 1024,
		WorkspaceRoot:  ws.ProjectDir,
	})

	// Create sanitizer.
	sanitizer := sandbox.NewSanitizer(100 * 1024)

	ctx := context.Background()

	// --all: smoke-test all enabled tools with empty params.
	if testAll {
		tools, err := store.ListTools()
		if err != nil {
			return fmt.Errorf("list tools: %w", err)
		}
		passed, failed := 0, 0
		for _, t := range tools {
			if t.State != "enabled" && t.State != "requires_confirmation" {
				continue
			}
			fmt.Fprintf(os.Stderr, "Testing %s... ", t.Name)
			result, err := executor.RunPythonFile(ctx, t.SourceFile, t.Name, "{}", "", nil)
			if err != nil || result.ExitCode != 0 {
				fmt.Fprintln(os.Stderr, "FAIL")
				failed++
			} else {
				fmt.Fprintln(os.Stderr, "OK")
				passed++
			}
		}
		fmt.Fprintf(os.Stderr, "\n%d passed, %d failed\n", passed, failed)
		if failed > 0 {
			return fmt.Errorf("%d tool(s) failed", failed)
		}
		return nil
	}

	// Single tool test.
	toolName := args[0]
	paramsStr, _ := cmd.Flags().GetString("params")

	// Validate params JSON.
	var params map[string]interface{}
	if err := json.Unmarshal([]byte(paramsStr), &params); err != nil {
		return fmt.Errorf("invalid --params JSON: %w", err)
	}

	// Get tool.
	tool, err := store.GetTool(toolName)
	if err != nil || tool == nil {
		return fmt.Errorf("tool %q not found; run 'foldermcp init' first", toolName)
	}

	// Enforce deny-by-default: only enabled and requires_confirmation tools can be tested.
	if !testForce {
		if tool.State == "disabled" {
			return fmt.Errorf("tool %q is disabled; run 'foldermcp review --approve=%s' first", toolName, toolName)
		}
		if tool.State == "pending" {
			return fmt.Errorf("tool %q is pending review; run 'foldermcp review --approve=%s' first", toolName, toolName)
		}
	} else {
		fmt.Fprintf(os.Stderr, "WARNING: --force flag set, bypassing tool state check\n")
	}

	// Run tool.
	fmt.Fprintf(os.Stderr, "Testing tool: %s\n", tool.Name)
	fmt.Fprintf(os.Stderr, "Source: %s\n", tool.SourceFile)
	fmt.Fprintf(os.Stderr, "Params: %s\n", paramsStr)
	fmt.Fprintln(os.Stderr, "---")

	result, err := executor.RunPythonFile(
		ctx,
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

	// Log the invocation to the audit log.
	status := "success"
	if result.ExitCode != 0 {
		status = "error"
	}
	if logger != nil {
		_ = logger.Log(tool.Name, "test", sanitizer.SanitizeParams(paramsStr), "", status)
	}

	if result.ExitCode != 0 {
		fmt.Fprintf(os.Stderr, "Stderr:\n%s\n", stderr)
		return fmt.Errorf("tool exited with code %d", result.ExitCode)
	}

	return nil
}
