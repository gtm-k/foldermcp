package router

import "testing"

func TestAnnotateCompletenessAddsWarning(t *testing.T) {
	r := &SearchResult{Completeness: "partial"}
	AnnotateCompleteness(r)
	if len(r.Warnings) == 0 {
		t.Error("expected warning when completeness=partial")
	}
}

func TestAnnotateCompletenessNoWarningOnFull(t *testing.T) {
	r := &SearchResult{Completeness: "full"}
	AnnotateCompleteness(r)
	if len(r.Warnings) != 0 {
		t.Error("no warning expected for full completeness")
	}
}

func TestAnnotateCompletenessPreservesExistingWarnings(t *testing.T) {
	r := &SearchResult{
		Completeness: "partial",
		Warnings:     []string{"existing warning"},
	}
	AnnotateCompleteness(r)
	if len(r.Warnings) != 1 || r.Warnings[0] != "existing warning" {
		t.Errorf("should not overwrite existing warnings, got %v", r.Warnings)
	}
}
