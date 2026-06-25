package main

import "path/filepath"

// ortLibName returns the platform-specific ONNX Runtime shared-library file
// name for the given GOOS.
func ortLibName(goos string) string {
	switch goos {
	case "windows":
		return "onnxruntime.dll"
	case "darwin":
		return "libonnxruntime.dylib"
	default:
		return "libonnxruntime.so"
	}
}

// resolveOrtLib decides which ONNX Runtime shared library to load, returning
// the chosen absolute/relative path and a short source label for logging.
// Priority:
//
//  1. env (FOLDERMCP_ORT_LIB) — trusted as-is, no stat, so an operator can
//     point at a vetted lib before it is staged;
//  2. <exeDir>/<lib>          — self-contained release bundle (lib beside exe);
//  3. <exeDir>/ort/<lib>      — bundle/dev layout with libs in an ort/ subdir
//     (e.g. the repo's bin/ort/). Probing this avoids a silent fall-through to
//     the system loader that can pick up a wrong-version DLL on PATH;
//  4. "" with source "system" — let the OS loader use its default search path.
//
// exists is injected (os.Stat in production) so the decision is unit-testable
// without touching the real filesystem or linking cgo.
func resolveOrtLib(env, exeDir, goos string, exists func(string) bool) (lib, source string) {
	if env != "" {
		return env, "env"
	}
	if exeDir != "" {
		name := ortLibName(goos)
		if cand := filepath.Join(exeDir, name); exists(cand) {
			return cand, "exe-adjacent"
		}
		if cand := filepath.Join(exeDir, "ort", name); exists(cand) {
			return cand, "exe-ort-subdir"
		}
	}
	return "", "system"
}
