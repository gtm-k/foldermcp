// Package sandbox provides subprocess execution with resource limits and
// output sanitization for running user-provided tool code.
package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ExecutorConfig controls resource limits for subprocess execution.
type ExecutorConfig struct {
	TimeoutSeconds int // Max execution time; default 30.
	MaxOutputBytes int // Max combined stdout+stderr; default 100KB.
}

// ExecutionResult captures the output of a subprocess invocation.
type ExecutionResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
}

// Executor runs Python code as subprocesses with resource limits.
type Executor struct {
	config ExecutorConfig
}

// NewExecutor creates an Executor with the given config.
// Zero-value fields get sensible defaults (30s timeout, 100KB output).
func NewExecutor(config ExecutorConfig) *Executor {
	if config.TimeoutSeconds <= 0 {
		config.TimeoutSeconds = 30
	}
	if config.MaxOutputBytes <= 0 {
		config.MaxOutputBytes = 100 * 1024
	}
	return &Executor{config: config}
}

// findPython locates a usable Python interpreter. It tries python3 first,
// then python, verifying the binary actually works (on Windows, python3 may
// be a Microsoft Store shim that does not execute).
func findPython() (string, error) {
	for _, name := range []string{"python3", "python"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		// Verify the binary actually runs (avoids Windows App Alias shims).
		cmd := exec.Command(path, "--version")
		if err := cmd.Run(); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("neither python3 nor python found in PATH")
}

// RunPython runs a Python code string as a subprocess.
// If venvPath is non-empty, the venv's bin directory is prepended to PATH.
// Additional environment variables can be passed via env.
func (e *Executor) RunPython(ctx context.Context, code string, venvPath string, env map[string]string) (*ExecutionResult, error) {
	pythonBin, err := findPython()
	if err != nil {
		return nil, err
	}

	timeout := time.Duration(e.config.TimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, pythonBin, "-c", code)

	// Build environment: inherit current env, add venv PATH, then extras.
	cmdEnv := os.Environ()
	if venvPath != "" {
		// Prepend venv bin to PATH.
		binDir := venvPath + "/bin"
		for i, v := range cmdEnv {
			if strings.HasPrefix(v, "PATH=") {
				cmdEnv[i] = "PATH=" + binDir + string(os.PathListSeparator) + v[5:]
				break
			}
		}
	}
	for k, v := range env {
		cmdEnv = append(cmdEnv, k+"="+v)
	}
	cmd.Env = cmdEnv

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	runErr := cmd.Run()
	duration := time.Since(start)

	// Check for timeout / context cancellation.
	if ctx.Err() != nil {
		return nil, fmt.Errorf("execution timeout after %v: %w", timeout, ctx.Err())
	}

	exitCode := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("failed to run python: %w", runErr)
		}
	}

	// Truncate output if necessary.
	stdoutStr := truncateBytes(stdout.Bytes(), e.config.MaxOutputBytes)
	stderrStr := truncateBytes(stderr.Bytes(), e.config.MaxOutputBytes)

	return &ExecutionResult{
		Stdout:   stdoutStr,
		Stderr:   stderrStr,
		ExitCode: exitCode,
		Duration: duration,
	}, nil
}

// RunPythonFile loads a Python file, calls a specific function with JSON args,
// and returns the JSON-encoded result.
func (e *Executor) RunPythonFile(ctx context.Context, filePath, funcName, argsJSON, venvPath string, env map[string]string) (*ExecutionResult, error) {
	// Build a wrapper script that imports the file, calls the function, and
	// prints the result as JSON.
	script := fmt.Sprintf(`import importlib.util, json, sys
spec = importlib.util.spec_from_file_location("_tool_module", %q)
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)
fn = getattr(mod, %q)
args = json.loads(%q)
result = fn(**args)
if result is None:
    print("null")
else:
    print(json.dumps(result))
`, filePath, funcName, argsJSON)

	return e.RunPython(ctx, script, venvPath, env)
}

// truncateBytes converts bytes to string, truncating if over limit.
func truncateBytes(b []byte, maxBytes int) string {
	if maxBytes > 0 && len(b) > maxBytes {
		return string(b[:maxBytes]) + fmt.Sprintf("... [output truncated at %d bytes]", maxBytes)
	}
	return string(b)
}
