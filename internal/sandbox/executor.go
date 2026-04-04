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
	TimeoutSeconds int    // Max execution time; default 30.
	MaxOutputBytes int    // Max combined stdout+stderr; default 100KB.
	MaxConcurrent  int    // Max concurrent subprocesses; default runtime.NumCPU().
	WorkspaceRoot  string // Root directory for path validation; empty disables the check.
}

// ExecutionResult captures the output of a subprocess invocation.
type ExecutionResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
}

// Executor runs Python code as subprocesses with resource limits.
// A channel-based semaphore gates how many subprocesses can run concurrently.
type Executor struct {
	config    ExecutorConfig
	semaphore chan struct{}
}

// NewExecutor creates an Executor with the given config.
// Zero-value fields get sensible defaults (30s timeout, 100KB output,
// NumCPU concurrent subprocesses).
func NewExecutor(config ExecutorConfig) *Executor {
	if config.TimeoutSeconds <= 0 {
		config.TimeoutSeconds = 30
	}
	if config.MaxOutputBytes <= 0 {
		config.MaxOutputBytes = 100 * 1024
	}
	maxConcurrent := config.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = runtime.NumCPU()
	}
	return &Executor{
		config:    config,
		semaphore: make(chan struct{}, maxConcurrent),
	}
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
	// Prevent Python from creating __pycache__ directories (important on NAS).
	if env == nil {
		env = make(map[string]string)
	}
	if _, exists := env["PYTHONDONTWRITEBYTECODE"]; !exists {
		env["PYTHONDONTWRITEBYTECODE"] = "1"
	}

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

	// Acquire semaphore slot before spawning subprocess.
	select {
	case e.semaphore <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("context cancelled waiting for semaphore: %w", ctx.Err())
	}

	start := time.Now()
	runErr := cmd.Run()
	duration := time.Since(start)

	// Release semaphore slot.
	<-e.semaphore

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

// validatePath checks that sourcePath is within workspaceRoot after resolving
// symlinks. This prevents path-traversal attacks and symlink escapes.
func validatePath(sourcePath, workspaceRoot string) error {
	absSource, err := filepath.Abs(sourcePath)
	if err != nil {
		return err
	}
	absSource, err = filepath.EvalSymlinks(absSource)
	if err != nil {
		return err
	}
	absRoot, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return err
	}
	absRoot, err = filepath.EvalSymlinks(absRoot)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(absSource, absRoot+string(filepath.Separator)) && absSource != absRoot {
		return fmt.Errorf("path %q is outside workspace %q", absSource, absRoot)
	}
	return nil
}

// RunPythonFile loads a Python file, calls a specific function with JSON args,
// and returns the JSON-encoded result.
// If WorkspaceRoot is set, .env variables from that directory are loaded and
// merged (per-tool env vars take precedence over .env values).
func (e *Executor) RunPythonFile(ctx context.Context, filePath, funcName, argsJSON, venvPath string, env map[string]string) (*ExecutionResult, error) {
	// Validate that the file is within the workspace boundary (Fix 1 + Fix 5:
	// EvalSymlinks inside validatePath also catches symlink escapes).
	if e.config.WorkspaceRoot != "" {
		if err := validatePath(filePath, e.config.WorkspaceRoot); err != nil {
			return nil, fmt.Errorf("path validation failed: %w", err)
		}
	}

	// Pass untrusted data via environment variables, not string interpolation.
	if env == nil {
		env = make(map[string]string)
	}

	// Load .env from workspace root, merging underneath per-tool env vars.
	if e.config.WorkspaceRoot != "" {
		dotEnv, err := LoadDotEnv(e.config.WorkspaceRoot)
		if err != nil {
			return nil, fmt.Errorf("load .env: %w", err)
		}
		for k, v := range dotEnv {
			if _, exists := env[k]; !exists {
				env[k] = v
			}
		}
	}

	env["_FOLDERMCP_FILE"] = filePath
	env["_FOLDERMCP_FUNC"] = funcName
	env["_FOLDERMCP_ARGS"] = argsJSON

	return e.RunPython(ctx, pythonFileScript, venvPath, env)
}

// LoadDotEnv reads a .env file from dir and returns the key-value pairs.
// Returns nil, nil if the file does not exist.
func LoadDotEnv(dir string) (map[string]string, error) {
	envPath := filepath.Join(dir, ".env")
	data, err := os.ReadFile(envPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	env := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			val = strings.Trim(val, `"'`)
			env[key] = val
		}
	}
	return env, nil
}

// truncateBytes converts bytes to string, truncating if over limit.
func truncateBytes(b []byte, maxBytes int) string {
	if maxBytes > 0 && len(b) > maxBytes {
		return string(b[:maxBytes]) + fmt.Sprintf("... [output truncated at %d bytes]", maxBytes)
	}
	return string(b)
}
