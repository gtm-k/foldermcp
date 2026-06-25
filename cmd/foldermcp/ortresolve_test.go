package main

import (
	"path/filepath"
	"testing"
)

// These tests pin the ONNX Runtime shared-library *resolution* logic, which is
// pure (no cgo, no real filesystem). The cgo wrapper configureOrtLib only adds
// SetSharedLibraryPath + logging on top of resolveOrtLib.

func TestOrtLibName(t *testing.T) {
	cases := map[string]string{
		"windows": "onnxruntime.dll",
		"darwin":  "libonnxruntime.dylib",
		"linux":   "libonnxruntime.so",
		"freebsd": "libonnxruntime.so", // default to the .so name
	}
	for goos, want := range cases {
		if got := ortLibName(goos); got != want {
			t.Errorf("ortLibName(%q) = %q, want %q", goos, got, want)
		}
	}
}

func TestResolveOrtLib(t *testing.T) {
	// exists simulates os.Stat success for a fixed set of present paths.
	existsFn := func(present ...string) func(string) bool {
		set := make(map[string]bool, len(present))
		for _, p := range present {
			set[p] = true
		}
		return func(p string) bool { return set[p] }
	}

	const exeDir = "exe-dir" // relative is fine; resolver only Joins onto it
	adjacent := func(goos string) string { return filepath.Join(exeDir, ortLibName(goos)) }
	ortSubdir := func(goos string) string { return filepath.Join(exeDir, "ort", ortLibName(goos)) }

	t.Run("env var wins and is trusted without stat", func(t *testing.T) {
		// Env path is used as-is even when the file is not (yet) present —
		// preserves the operator's explicit override.
		envPath := filepath.Join("vetted", "onnxruntime.dll")
		lib, source := resolveOrtLib(envPath, exeDir, "linux", existsFn())
		if lib != envPath || source != "env" {
			t.Fatalf("got (%q, %q), want (%q, env)", lib, source, envPath)
		}
	})

	t.Run("dll beside the executable is selected", func(t *testing.T) {
		want := adjacent("windows")
		lib, source := resolveOrtLib("", exeDir, "windows", existsFn(want))
		if lib != want || source != "exe-adjacent" {
			t.Fatalf("got (%q, %q), want (%q, exe-adjacent)", lib, source, want)
		}
	})

	t.Run("dll in ort subdir beside the executable is selected", func(t *testing.T) {
		// This is the dev `bin/ort/` layout that previously fell through to the
		// system loader and silently picked up a wrong-version DLL on PATH.
		want := ortSubdir("windows")
		lib, source := resolveOrtLib("", exeDir, "windows", existsFn(want))
		if lib != want || source != "exe-ort-subdir" {
			t.Fatalf("got (%q, %q), want (%q, exe-ort-subdir)", lib, source, want)
		}
	})

	t.Run("adjacent dll takes priority over ort subdir", func(t *testing.T) {
		want := adjacent("linux")
		lib, source := resolveOrtLib("", exeDir, "linux", existsFn(want, ortSubdir("linux")))
		if lib != want || source != "exe-adjacent" {
			t.Fatalf("got (%q, %q), want (%q, exe-adjacent)", lib, source, want)
		}
	})

	t.Run("nothing bundled falls back to the system loader", func(t *testing.T) {
		lib, source := resolveOrtLib("", exeDir, "linux", existsFn())
		if lib != "" || source != "system" {
			t.Fatalf("got (%q, %q), want (\"\", system)", lib, source)
		}
	})

	t.Run("empty exeDir falls back to the system loader", func(t *testing.T) {
		lib, source := resolveOrtLib("", "", "linux", existsFn(adjacent("linux")))
		if lib != "" || source != "system" {
			t.Fatalf("got (%q, %q), want (\"\", system)", lib, source)
		}
	})
}
