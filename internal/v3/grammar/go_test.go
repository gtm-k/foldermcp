//go:build cgo

package grammar

import "testing"

func TestGoExtractorSymbols(t *testing.T) {
	src := []byte(`package demo

import "fmt"

func Hello() string {
    return "hi"
}

type User struct {
    Name string
}

func (u *User) Greet() string {
    return fmt.Sprintf("hi %s", u.Name)
}

type Greeter interface {
    Greet() string
}
`)
	e, err := NewGoExtractor()
	if err != nil {
		t.Fatalf("NewGoExtractor: %v", err)
	}
	syms, err := e.Extract(src)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"Hello":   "function",
		"User":    "struct",
		"Greet":   "method",
		"Greeter": "interface",
	}
	got := make(map[string]string)
	for _, s := range syms {
		got[s.Name] = s.Kind
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %q, want %q", k, got[k], v)
		}
	}
}

func TestGoExtractorByteOffsets(t *testing.T) {
	src := []byte("package x\nfunc Foo() {}\n")
	e, err := NewGoExtractor()
	if err != nil {
		t.Fatal(err)
	}
	syms, err := e.Extract(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(syms) != 1 {
		t.Fatalf("got %d symbols, want 1", len(syms))
	}
	s := syms[0]
	if s.Name != "Foo" {
		t.Errorf("name = %q, want Foo", s.Name)
	}
	if s.ByteStart >= s.ByteEnd {
		t.Errorf("byte range invalid: [%d, %d)", s.ByteStart, s.ByteEnd)
	}
	body := string(src[s.ByteStart:s.ByteEnd])
	if body != "func Foo() {}" {
		t.Errorf("body = %q", body)
	}
}
