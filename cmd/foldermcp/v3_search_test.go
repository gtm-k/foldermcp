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

func TestFormatSearchResultsOmitsMisleadingRRFScore(t *testing.T) {
	// The per-result Score is the Reciprocal Rank Fusion score — a positional
	// artifact (1/(k+rank+1)), NOT a relevance magnitude. Rendering it as a
	// 4-decimal number invites readers to compare hits by a number that only
	// encodes rank. The human output must instead convey order (rank ordinal)
	// and source agreement (how many independent sources matched), and must not
	// leak the raw RRF float.
	resp := &pb.SearchBroadlyResponse{
		OverallStatus: "ok",
		Completeness:  "full",
		Results: []*pb.SearchHit{
			{Path: "/a.go", Score: 0.0323, MatchedSources: []string{"fts", "vector"}},
			{Path: "/b.go", Score: 0.0164, MatchedSources: []string{"vector"}},
		},
	}
	out := formatSearchResults(resp, "q")

	for _, leak := range []string{"0.0323", "0.0164"} {
		if strings.Contains(out, leak) {
			t.Errorf("raw RRF score %q must not appear in human output:\n%s", leak, out)
		}
	}
	if !strings.Contains(out, "1.") || !strings.Contains(out, "2.") {
		t.Errorf("rank ordinals must remain visible:\n%s", out)
	}
	if !strings.Contains(out, "(fts+vector)") || !strings.Contains(out, "(vector)") {
		t.Errorf("source agreement must remain visible:\n%s", out)
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

func TestSanitizeTerminalStripsC1Controls(t *testing.T) {
	// C1 control codes (U+0080–U+009F) include single-byte escape introducers —
	// CSI (U+009B), OSC (U+009D), DCS (U+0090) — that a crafted indexed file could
	// use to inject terminal sequences, bypassing the ESC (0x1b) strip. They must
	// be removed just like C0 controls.
	in := "ok\u009b31mRED\u009dhijack\u0090dcs\u0080\u009fend"
	got := sanitizeTerminal(in)
	for _, bad := range []rune{0x80, 0x90, 0x9b, 0x9d, 0x9f} {
		if strings.ContainsRune(got, bad) {
			t.Errorf("C1 control %#x survived sanitization: %q", bad, got)
		}
	}
	// Printable text on either side of the stripped controls must survive.
	for _, w := range []string{"ok", "RED", "hijack", "dcs", "end"} {
		if !strings.Contains(got, w) {
			t.Errorf("printable text %q must remain: %q", w, got)
		}
	}
}

func TestSanitizeTerminalStripsLineSepAndBiDi(t *testing.T) {
	// Unicode line/paragraph separators (U+2028/U+2029) and BiDi override/isolate
	// controls (U+202A–202E, U+2066–2069, the "Trojan Source" class) must be
	// stripped — they enable line-spoofing and visual reordering in terminals.
	in := "a\u2028b\u2029c\u202ad\u202ee\u2066f\u2069g"
	got := sanitizeTerminal(in)
	for _, bad := range []rune{0x2028, 0x2029, 0x202a, 0x202e, 0x2066, 0x2069} {
		if strings.ContainsRune(got, bad) {
			t.Errorf("format control %#x survived sanitization: %q", bad, got)
		}
	}
	for _, w := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		if !strings.Contains(got, w) {
			t.Errorf("printable text %q must remain: %q", w, got)
		}
	}
}

func TestSanitizeLineDropsNewlinesAndControls(t *testing.T) {
	// sanitizeLine is for single-line fields (path, title): it drops newlines and
	// tabs (so a crafted value cannot inject extra output lines) AND everything
	// sanitizeTerminal strips (C0/DEL/C1/line-sep/BiDi).
	got := sanitizeLine("path/to\nFAKE\trest\u009bx\u202ey")
	if strings.ContainsAny(got, "\n\t") {
		t.Errorf("sanitizeLine must drop newlines/tabs: %q", got)
	}
	for _, bad := range []rune{0x009b, 0x202e} {
		if strings.ContainsRune(got, bad) {
			t.Errorf("sanitizeLine must also strip control %#x: %q", bad, got)
		}
	}
	if !strings.Contains(got, "FAKE") || !strings.Contains(got, "rest") {
		t.Errorf("printable text must remain: %q", got)
	}
}

func TestFormatSearchResultsPathTitleAreSingleLine(t *testing.T) {
	// A crafted path/title with an embedded newline must not inject a separate
	// output line (terminal/CI log spoofing).
	resp := &pb.SearchBroadlyResponse{
		OverallStatus: "ok",
		Completeness:  "full",
		Results: []*pb.SearchHit{
			{Path: "real.go\nINJECTED", Title: "Title\nALSOINJECTED", MatchedSources: []string{"fts"}},
		},
	}
	out := formatSearchResults(resp, "q")
	if strings.Contains(out, "real.go\nINJECTED") || strings.Contains(out, "Title\nALSOINJECTED") {
		t.Errorf("path/title newline must be stripped (line-injection vector):\n%q", out)
	}
	if !strings.Contains(out, "real.goINJECTED") {
		t.Errorf("expected path rendered with newline removed:\n%q", out)
	}
}
