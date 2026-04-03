package deps

import (
	"testing"
)

func TestLoadMapping(t *testing.T) {
	pm, err := LoadMapping()
	if err != nil {
		t.Fatalf("LoadMapping() error: %v", err)
	}
	if pm == nil {
		t.Fatal("LoadMapping() returned nil")
	}
	if len(pm.mapping) < 60 {
		t.Errorf("expected at least 60 mappings, got %d", len(pm.mapping))
	}
}

func TestResolvePackageName(t *testing.T) {
	pm, err := LoadMapping()
	if err != nil {
		t.Fatalf("LoadMapping() error: %v", err)
	}

	tests := []struct {
		importName  string
		wantPackage string
		wantFound   bool
	}{
		{"cv2", "opencv-python", true},
		{"PIL", "Pillow", true},
		{"pandas", "pandas", true},
		{"yaml", "PyYAML", true},
		{"sklearn", "scikit-learn", true},
		{"bs4", "beautifulsoup4", true},
		{"requests", "requests", true},
		{"numpy", "numpy", true},
		{"flask", "Flask", true},
		{"jinja2", "Jinja2", true},
		{"psycopg2", "psycopg2-binary", true},
		{"dotenv", "python-dotenv", true},
		// Unknown package should fall back to import name
		{"some_unknown_pkg_xyz", "some_unknown_pkg_xyz", false},
		{"another_random_thing", "another_random_thing", false},
	}

	for _, tt := range tests {
		t.Run(tt.importName, func(t *testing.T) {
			gotPkg, gotFound := pm.Resolve(tt.importName)
			if gotPkg != tt.wantPackage {
				t.Errorf("Resolve(%q) package = %q, want %q", tt.importName, gotPkg, tt.wantPackage)
			}
			if gotFound != tt.wantFound {
				t.Errorf("Resolve(%q) found = %v, want %v", tt.importName, gotFound, tt.wantFound)
			}
		})
	}
}
