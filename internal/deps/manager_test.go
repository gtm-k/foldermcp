package deps

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestManager_RequirementsTxtExists(t *testing.T) {
	dir := t.TempDir()

	// Create a requirements.txt with some packages.
	content := "requests==2.31.0\npandas>=2.0\nnumpy\n"
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager()
	imports := []ImportedPackage{
		{ImportName: "requests"},
		{ImportName: "pandas"},
	}

	result, err := mgr.Resolve(dir, imports)
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}

	if result.Strategy != "requirements.txt" {
		t.Errorf("Strategy = %q, want %q", result.Strategy, "requirements.txt")
	}

	// Packages from requirements.txt should be present.
	if len(result.Packages) != 3 {
		t.Errorf("expected 3 packages, got %d", len(result.Packages))
	}

	// Check that the package names are extracted from requirements.txt.
	foundRequests := false
	foundPandas := false
	foundNumpy := false
	for _, pkg := range result.Packages {
		switch pkg.PyPIName {
		case "requests":
			foundRequests = true
		case "pandas":
			foundPandas = true
		case "numpy":
			foundNumpy = true
		}
	}
	if !foundRequests || !foundPandas || !foundNumpy {
		t.Errorf("missing expected packages: requests=%v pandas=%v numpy=%v",
			foundRequests, foundPandas, foundNumpy)
	}
}

func TestManager_PyprojectTomlExists(t *testing.T) {
	dir := t.TempDir()

	// Create a pyproject.toml with dependencies.
	content := `[project]
name = "my-project"
version = "1.0.0"
dependencies = [
    "fastapi>=0.100.0",
    "uvicorn",
    "pydantic>=2.0",
]
`
	if err := os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager()
	imports := []ImportedPackage{
		{ImportName: "fastapi"},
	}

	result, err := mgr.Resolve(dir, imports)
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}

	if result.Strategy != "pyproject.toml" {
		t.Errorf("Strategy = %q, want %q", result.Strategy, "pyproject.toml")
	}

	if len(result.Packages) != 3 {
		t.Errorf("expected 3 packages, got %d", len(result.Packages))
	}

	foundFastapi := false
	foundUvicorn := false
	foundPydantic := false
	for _, pkg := range result.Packages {
		switch pkg.PyPIName {
		case "fastapi":
			foundFastapi = true
		case "uvicorn":
			foundUvicorn = true
		case "pydantic":
			foundPydantic = true
		}
	}
	if !foundFastapi || !foundUvicorn || !foundPydantic {
		t.Errorf("missing expected packages: fastapi=%v uvicorn=%v pydantic=%v",
			foundFastapi, foundUvicorn, foundPydantic)
	}
}

func TestManager_RequirementsTxtPriority(t *testing.T) {
	dir := t.TempDir()

	// Both files present: requirements.txt should win.
	reqContent := "flask==3.0.0\n"
	pyContent := `[project]
name = "my-project"
dependencies = ["django>=4.0"]
`
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte(reqContent), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte(pyContent), 0644); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager()
	result, err := mgr.Resolve(dir, nil)
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}

	if result.Strategy != "requirements.txt" {
		t.Errorf("Strategy = %q, want %q (requirements.txt should take priority)", result.Strategy, "requirements.txt")
	}
}

func TestManager_InferFromImports(t *testing.T) {
	mgr := NewManager()
	imports := []ImportedPackage{
		{ImportName: "pandas"},
		{ImportName: "requests"},
		{ImportName: "cv2"},
		{ImportName: "unknown_package_xyz"},
	}

	packages, unresolved := mgr.MapImports(imports)

	if len(packages) != 4 {
		t.Fatalf("expected 4 packages, got %d", len(packages))
	}

	// Check specific mappings.
	pkgMap := make(map[string]ResolvedPackage)
	for _, p := range packages {
		pkgMap[p.ImportName] = p
	}

	// pandas -> pandas (same name, resolved)
	if p, ok := pkgMap["pandas"]; !ok || p.PyPIName != "pandas" || !p.Resolved {
		t.Errorf("pandas mapping incorrect: %+v", pkgMap["pandas"])
	}

	// cv2 -> opencv-python (different name, resolved)
	if p, ok := pkgMap["cv2"]; !ok || p.PyPIName != "opencv-python" || !p.Resolved {
		t.Errorf("cv2 mapping incorrect: %+v", pkgMap["cv2"])
	}

	// unknown -> fallback to import name, not resolved
	if p, ok := pkgMap["unknown_package_xyz"]; !ok || p.PyPIName != "unknown_package_xyz" || p.Resolved {
		t.Errorf("unknown_package_xyz mapping incorrect: %+v", pkgMap["unknown_package_xyz"])
	}

	// Unresolved should include the unknown package.
	if len(unresolved) != 1 || unresolved[0] != "unknown_package_xyz" {
		t.Errorf("unresolved = %v, want [unknown_package_xyz]", unresolved)
	}
}

func TestManager_InferStrategy(t *testing.T) {
	// No requirements.txt or pyproject.toml: should infer from imports.
	dir := t.TempDir()

	mgr := NewManager()
	imports := []ImportedPackage{
		{ImportName: "flask"},
		{ImportName: "requests"},
	}

	result, err := mgr.Resolve(dir, imports)
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}

	if result.Strategy != "inferred" {
		t.Errorf("Strategy = %q, want %q", result.Strategy, "inferred")
	}

	if len(result.Packages) != 2 {
		t.Errorf("expected 2 packages, got %d", len(result.Packages))
	}
}

func TestManager_NoneStrategy(t *testing.T) {
	// No dep files and no imports: strategy should be "none".
	dir := t.TempDir()

	mgr := NewManager()
	result, err := mgr.Resolve(dir, nil)
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}

	if result.Strategy != "none" {
		t.Errorf("Strategy = %q, want %q", result.Strategy, "none")
	}

	if len(result.Packages) != 0 {
		t.Errorf("expected 0 packages, got %d", len(result.Packages))
	}
}

func TestManager_CreateVenv(t *testing.T) {
	// Skip if uv is not installed.
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not installed, skipping integration test")
	}

	dir := t.TempDir()

	mgr := NewManager()
	venvPath, err := mgr.CreateVenv(dir, []string{"requests"})
	if err != nil {
		t.Fatalf("CreateVenv() error: %v", err)
	}

	expectedPath := filepath.Join(dir, ".foldermcp", "venv")
	if venvPath != expectedPath {
		t.Errorf("venvPath = %q, want %q", venvPath, expectedPath)
	}

	// Verify the venv directory was created.
	if _, err := os.Stat(venvPath); os.IsNotExist(err) {
		t.Errorf("venv directory was not created at %q", venvPath)
	}

	// Verify a Python executable exists in the venv.
	// On Windows it's Scripts/python.exe, on Unix it's bin/python.
	pythonPaths := []string{
		filepath.Join(venvPath, "bin", "python"),
		filepath.Join(venvPath, "bin", "python3"),
		filepath.Join(venvPath, "Scripts", "python.exe"),
	}
	found := false
	for _, p := range pythonPaths {
		if _, err := os.Stat(p); err == nil {
			found = true
			break
		}
	}
	if !found {
		t.Error("no python executable found in venv")
	}
}
