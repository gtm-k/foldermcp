package pythonrt

import (
	"fmt"
	"os/exec"
	"sync"
)

var (
	pythonPath string
	pythonOnce sync.Once
	pythonErr  error
)

// FindPython returns the path to the Python interpreter, caching the result.
// Tries python3 first, then python. Validates the binary actually runs.
func FindPython() (string, error) {
	pythonOnce.Do(func() {
		for _, name := range []string{"python3", "python"} {
			path, err := exec.LookPath(name)
			if err != nil {
				continue
			}
			// Verify it actually runs (Windows App Alias shim detection)
			cmd := exec.Command(path, "--version")
			if err := cmd.Run(); err != nil {
				continue
			}
			pythonPath = path
			return
		}
		pythonErr = fmt.Errorf("python not found: install Python 3.10+ from https://python.org")
	})
	return pythonPath, pythonErr
}
