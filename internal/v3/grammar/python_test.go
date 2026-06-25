//go:build cgo

package grammar

import "testing"

func TestPythonExtractor(t *testing.T) {
	src := []byte(`
def top_level():
    pass

class Dog:
    def bark(self):
        return "woof"

    def sit(self):
        return "sat"

def another():
    pass
`)
	e, err := NewPythonExtractor()
	if err != nil {
		t.Fatalf("NewPythonExtractor: %v", err)
	}
	syms, err := e.Extract(src)
	if err != nil {
		t.Fatal(err)
	}
	kinds := make(map[string]string)
	for _, s := range syms {
		kinds[s.Name] = s.Kind
	}
	checks := map[string]string{
		"top_level": "function",
		"Dog":       "class",
		"bark":      "method",
		"sit":       "method",
		"another":   "function",
	}
	for k, v := range checks {
		if kinds[k] != v {
			t.Errorf("%s: got %q want %q", k, kinds[k], v)
		}
	}
}

func TestPythonExtractorEmptyFile(t *testing.T) {
	e, err := NewPythonExtractor()
	if err != nil {
		t.Fatal(err)
	}
	syms, err := e.Extract([]byte(""))
	if err != nil {
		t.Fatal(err)
	}
	if len(syms) != 0 {
		t.Errorf("got %d symbols from empty file", len(syms))
	}
}
