//go:build cgo

package chunker

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestChunkDataJSON(t *testing.T) {
	jsonDoc := `{
  "service": "indexer",
  "version": 3,
  "settings": {"timeout": 30, "retries": 5, "endpoint": "https://example.test/api"},
  "tags": ["semantic", "retrieval", "local"],
  "nested": {"deep": {"key": "tungsten alloy probe phrase"}}
}`
	path := writeTemp(t, "config.json", jsonDoc)
	chunks, stats, err := ChunkData(path, "config.json", ".json", DefaultConfig(), wordCounter{})
	if err != nil {
		t.Fatalf("ChunkData json: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("no chunks from json")
	}
	for _, c := range chunks {
		if c.Kind != "data_structured" {
			t.Errorf("kind = %q, want data_structured", c.Kind)
		}
		if c.ByteStart != 0 || c.ByteEnd != len(c.Text) {
			t.Errorf("byte span (%d,%d) not self-relative", c.ByteStart, c.ByteEnd)
		}
	}
	// The outline must surface key paths so retrieval can match on them.
	joined := allText(chunks)
	for _, want := range []string{"service", "settings.timeout", "tungsten alloy probe phrase"} {
		if !strings.Contains(joined, want) {
			t.Errorf("json outline missing %q\n%s", want, joined)
		}
	}
	if stats.ExtractorVersion == "" {
		t.Error("stats.ExtractorVersion empty")
	}
}

func TestChunkDataYAML(t *testing.T) {
	yamlDoc := `service: indexer
version: 3
settings:
  timeout: 30
  endpoint: https://example.test/api
tags:
  - semantic
  - retrieval
nested:
  deep:
    key: cobalt isotope probe phrase
`
	path := writeTemp(t, "config.yaml", yamlDoc)
	chunks, _, err := ChunkData(path, "config.yaml", ".yaml", DefaultConfig(), wordCounter{})
	if err != nil {
		t.Fatalf("ChunkData yaml: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("no chunks from yaml")
	}
	joined := allText(chunks)
	for _, want := range []string{"service", "settings.timeout", "cobalt isotope probe phrase"} {
		if !strings.Contains(joined, want) {
			t.Errorf("yaml outline missing %q\n%s", want, joined)
		}
	}
}

func TestChunkDataXML(t *testing.T) {
	xmlDoc := `<?xml version="1.0"?>
<config>
  <service>indexer</service>
  <version>3</version>
  <settings>
    <timeout>30</timeout>
    <endpoint>https://example.test/api</endpoint>
  </settings>
  <note>molybdenum filament probe phrase</note>
</config>`
	path := writeTemp(t, "config.xml", xmlDoc)
	chunks, _, err := ChunkData(path, "config.xml", ".xml", DefaultConfig(), wordCounter{})
	if err != nil {
		t.Fatalf("ChunkData xml: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("no chunks from xml")
	}
	joined := allText(chunks)
	for _, want := range []string{"service", "molybdenum filament probe phrase"} {
		if !strings.Contains(joined, want) {
			t.Errorf("xml outline missing %q\n%s", want, joined)
		}
	}
}

// TestChunkDataMalformedFallsBack: an unparseable json/yaml/xml returns
// ErrDataFallback so the runner prose-chunks it and marks the node AMBIGUOUS.
func TestChunkDataMalformedFallsBack(t *testing.T) {
	cases := []struct {
		name, ext, body string
	}{
		{"bad.json", ".json", `{"unterminated": `},
		{"bad.xml", ".xml", `<root><unclosed></root>`},
		{"bad.yaml", ".yaml", "key: value\n  bad:\n- : : :\n\t\ttabs"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writeTemp(t, c.name, c.body)
			_, _, err := ChunkData(path, c.name, c.ext, DefaultConfig(), wordCounter{})
			if !errors.Is(err, ErrDataFallback) {
				t.Fatalf("%s: err = %v, want ErrDataFallback", c.name, err)
			}
		})
	}
}

// TestChunkDataEmptyStructure: a well-formed but empty JSON object {} / array []
// (zero leaves) is NOT malformed — it must return ErrDataEmpty (which still wraps
// ErrDataFallback so the runner prose-falls-back) so the runner labels the node
// fallback_reason="empty_structure", not "malformed" (FIX 3).
func TestChunkDataEmptyStructure(t *testing.T) {
	cases := []struct {
		name, ext, body string
	}{
		{"empty-obj.json", ".json", `{}`},
		{"empty-arr.json", ".json", `[]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writeTemp(t, c.name, c.body)
			_, _, err := ChunkData(path, c.name, c.ext, DefaultConfig(), wordCounter{})
			if !errors.Is(err, ErrDataEmpty) {
				t.Fatalf("%s: err = %v, want ErrDataEmpty", c.name, err)
			}
			// ErrDataEmpty must remain a fallback so the runner's existing
			// ErrDataFallback dispatch still prose-falls-back rather than failing.
			if !errors.Is(err, ErrDataFallback) {
				t.Fatalf("%s: ErrDataEmpty must wrap ErrDataFallback, got %v", c.name, err)
			}
		})
	}
}

// TestChunkDataTrailingBraceRejected (A5 LOW gap): a JSON value with a trailing
// bare "}" or "]" after the first complete value is rejected by the io.EOF check
// — it is malformed (ErrDataFallback) and NOT a valid empty structure.
func TestChunkDataTrailingBraceRejected(t *testing.T) {
	cases := []struct{ name, body string }{
		{"trailing-brace.json", `{"a":1}}`},
		{"trailing-bracket.json", `[1,2]]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writeTemp(t, c.name, c.body)
			_, _, err := ChunkData(path, c.name, ".json", DefaultConfig(), wordCounter{})
			if !errors.Is(err, ErrDataFallback) {
				t.Fatalf("%s: err = %v, want ErrDataFallback (trailing garbage)", c.name, err)
			}
			// A trailing bare brace is malformed, NOT an empty structure.
			if errors.Is(err, ErrDataEmpty) {
				t.Fatalf("%s: trailing garbage must NOT be ErrDataEmpty", c.name)
			}
		})
	}
}

// TestChunkDataBoundedChunkCount: a deeply nested / large structure produces a
// bounded number of chunks (never unbounded), via the per-file chunk cap.
func TestChunkDataBoundedChunkCount(t *testing.T) {
	var b strings.Builder
	b.WriteString("{")
	for i := 0; i < 50000; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "\"key_%d\": \"value number %d with some words\"", i, i)
	}
	b.WriteString("}")
	path := writeTemp(t, "huge.json", b.String())
	chunks, _, err := ChunkData(path, "huge.json", ".json", DefaultConfig(), wordCounter{})
	if err != nil && !errors.Is(err, ErrDataFallback) {
		t.Fatalf("ChunkData huge: %v", err)
	}
	if len(chunks) > maxChunksPerDoc {
		t.Errorf("chunks = %d, exceeds cap %d", len(chunks), maxChunksPerDoc)
	}
}

func allText(chunks []DataChunk) string {
	var b strings.Builder
	for _, c := range chunks {
		b.WriteString(c.Header)
		b.WriteByte('\n')
		b.WriteString(c.Text)
		b.WriteByte('\n')
	}
	return b.String()
}
