// Package sandbox provides subprocess execution with resource limits and
// output sanitization for running user-provided tool code.
package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/foldermcp/foldermcp/internal/pythonrt"
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

// findPython delegates to the shared pythonrt package.
func findPython() (string, error) {
	return pythonrt.FindPython()
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
		// Prepend venv bin to PATH (Scripts on Windows, bin elsewhere).
		binDir := filepath.Join(venvPath, "bin")
		if runtime.GOOS == "windows" {
			binDir = filepath.Join(venvPath, "Scripts")
		}
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

// pythonFileScript is a static Python script that reads file path, function
// name, and arguments from environment variables. This avoids string
// interpolation of untrusted data into the script body.
const pythonFileScript = `import asyncio, importlib.util, json, sys, os
spec = importlib.util.spec_from_file_location("_tool_module", os.environ["_FOLDERMCP_FILE"])
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)
fn = getattr(mod, os.environ["_FOLDERMCP_FUNC"])
args = json.loads(os.environ.get("_FOLDERMCP_ARGS", "{}"))
result = fn(**args)
if asyncio.iscoroutine(result):
    result = asyncio.run(result)
print("null" if result is None else json.dumps(result))
`

// RunPythonFile loads a Python file, calls a specific function with JSON args,
// and returns the JSON-encoded result.
func (e *Executor) RunPythonFile(ctx context.Context, filePath, funcName, argsJSON, venvPath string, env map[string]string) (*ExecutionResult, error) {
	// Pass untrusted data via environment variables, not string interpolation.
	if env == nil {
		env = make(map[string]string)
	}
	env["_FOLDERMCP_FILE"] = filePath
	env["_FOLDERMCP_FUNC"] = funcName
	env["_FOLDERMCP_ARGS"] = argsJSON

	return e.RunPython(ctx, pythonFileScript, venvPath, env)
}

// truncateBytes converts bytes to string, truncating if over limit.
func truncateBytes(b []byte, maxBytes int) string {
	if maxBytes > 0 && len(b) > maxBytes {
		return string(b[:maxBytes]) + fmt.Sprintf("... [output truncated at %d bytes]", maxBytes)
	}
	return string(b)
}
