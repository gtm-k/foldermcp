//go:build cgo

package main

import (
	"strings"
	"testing"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
)

func TestFormatSearchResultsWithHits(t *testing.T) {
	resp := &pb.SearchBroadlyResponse{
		OverallStatus: "ok",
		Completeness:  "full",
		Sources: []*pb.SourceStatus{
			{SourceName: "fts", Status: "OK", LatencyMs: 2},
			{SourceName: "vector", Status: "OK", LatencyMs: 15},
		},
		Results: []*pb.SearchHit{
			{Path: "/repo/config.go", Score: 0.0323, MatchedSources: []string{"fts", "vector"}, Snippet: "type Config struct"},
		},
	}
	out := formatSearchResults(resp, "database config")

	for _, want := range []string{
		`query "database config"`,
		"1 result",
		"status=ok",
		"fts=OK(2ms)",
		"vector=OK(15ms)",
		"/repo/config.go",
		"(fts+vector)",
		"type Config struct",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
}

func TestFormatSearchResultsNoHits(t *testing.T) {
	resp := &pb.SearchBroadlyResponse{OverallStatus: "ok", Completeness: "full"}
	out := formatSearchResults(resp, "zzz")
	if !strings.Contains(out, "no matches") {
		t.Errorf("expected a clear no-matches message, got:\n%s", out)
	}
}

func TestValidSearchMode(t *testing.T) {
	for _, m := range []string{"auto", "lexical", "semantic", "filename"} {
		if !validSearchMode(m) {
			t.Errorf("mode %q should be valid", m)
		}
	}
	for _, m := range []string{"", "fuzzy", "Auto", "vector"} {
		if validSearchMode(m) {
			t.Errorf("mode %q should be invalid", m)
		}
	}
}

func TestSanitizeTerminalStripsEscapes(t *testing.T) {
	// A crafted snippet with an ANSI escape and a NUL must be neutralized.
	in := "before\x1b[31mRED\x1b[0m\x00after\nline2\twrapped"
	got := sanitizeTerminal(in)
	if strings.ContainsRune(got, 0x1b) || strings.ContainsRune(got, 0x00) {
		t.Errorf("control chars survived: %q", got)
	}
	if !strings.Contains(got, "\n") || !strings.Contains(got, "\t") {
		t.Errorf("newline/tab must be preserved: %q", got)
	}
	// Stripping ESC neutralizes the ANSI sequence; the literal "[31m" text may
	// remain (harmless without ESC). The human-readable words must survive.
	for _, w := range []string{"before", "RED", "after"} {
		if !strings.Contains(got, w) {
			t.Errorf("printable text %q must remain: %q", w, got)
		}
	}
}
