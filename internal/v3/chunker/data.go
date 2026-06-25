//go:build cgo

package chunker

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	yaml "gopkg.in/yaml.v3"
)

// dataExtractorVersion tags the on-disk provenance of structured-data outlines
// (R7: not in the embedding fingerprint, so a future output change needs an
// explicit reindex policy).
const dataExtractorVersion = "data-outline-v1"

// maxDataInputBytes bounds the bytes the structured-data chunker reads + parses
// from a file. JSON/YAML/XML are already UTF-8 text, so this mirrors the prose
// path's 32 MB source-read cap intent: a pathological multi-GB document never
// slurps into memory. A file larger than this falls back to prose (the runner's
// readFileContent cap would reject it anyway).
const maxDataInputBytes = 32 * 1024 * 1024

// maxOutlineLines caps the key-path/value outline a single document can expand
// to BEFORE chunking. A document with millions of leaves would otherwise build
// a huge outline string and then a huge chunk slice. Bounding the outline first
// bounds both. 200k lines is far beyond any legitimate config/data document yet
// keeps the worst case linear and small.
const maxOutlineLines = 200000

// ChunkData reads a JSON/YAML/XML file, normalizes it into a stable key-path /
// value outline, and chunks that outline with the recursive prose splitter,
// emitting data_structured chunks (D18). It is a structure-aware chunker, NOT a
// binary extractor — these formats are already UTF-8 text; the value added is a
// flattened, retrieval-friendly outline (key paths as headers, leaf values as
// text) rather than raw nested syntax.
//
// Security (consistent with A4): input is size-bounded (maxDataInputBytes); XML
// uses encoding/xml, which does NOT resolve external entities by default (no XXE
// — we never enable dec.Entity expansion or a custom resolver); a per-document
// chunk cap (maxChunksPerDoc, shared with the office extractor) bounds output.
//
// On any parse failure (malformed json/yaml/xml) it returns ErrDataFallback so
// the runner prose-chunks the raw file and marks the node provenance AMBIGUOUS.
// Never panics.
func ChunkData(path, title, ext string, cfg Config, counter Counter) ([]DataChunk, DataStats, error) {
	if cfg == (Config{}) {
		cfg = DefaultConfig()
	}
	raw, truncated, err := readBounded(path, maxDataInputBytes)
	stats := DataStats{ExtractorVersion: dataExtractorVersion, ScannedBytes: len(raw), Truncated: truncated}
	if err != nil {
		return nil, stats, err
	}
	if truncated {
		// Oversize structured data: the parsers need the whole document to be
		// well-formed, and a truncated tail would fail to parse anyway. Fall back
		// to prose rather than reject the file entirely.
		return nil, stats, fmt.Errorf("data oversize %s: %w", path, ErrDataFallback)
	}

	var lines []outlineLine
	switch strings.ToLower(ext) {
	case ".json":
		lines, err = jsonOutline(raw)
	case ".yaml", ".yml":
		lines, err = yamlOutline(raw)
	case ".xml":
		lines, err = xmlOutline(raw)
	default:
		return nil, stats, fmt.Errorf("data unsupported ext %q: %w", ext, ErrDataFallback)
	}
	if err != nil {
		return nil, stats, fmt.Errorf("data parse %s: %w", path, ErrDataFallback)
	}
	if len(lines) == 0 {
		return nil, stats, fmt.Errorf("data empty %s: %w", path, ErrDataFallback)
	}

	// Build the outline text: one "path: value" line per leaf. The whole outline
	// is then prose-chunked so chunk sizing matches the rest of the corpus.
	outline := renderOutline(lines)
	prose := ChunkProse(outline, cfg, counter)
	if len(prose) == 0 {
		return nil, stats, fmt.Errorf("data no-chunks %s: %w", path, ErrDataFallback)
	}

	out := make([]DataChunk, 0, len(prose))
	for _, pc := range prose {
		out = append(out, DataChunk{
			Text:       pc.Text,
			ByteStart:  0,
			ByteEnd:    len(pc.Text),
			TokenCount: pc.TokenCount,
			Header:     fmt.Sprintf("%s structure — %s", strings.TrimPrefix(strings.ToLower(ext), "."), title),
			Kind:       "data_structured",
		})
		if len(out) > maxChunksPerDoc {
			return nil, stats, fmt.Errorf("data too many chunks %s: %w", path, ErrDataFallback)
		}
	}
	return out, stats, nil
}

// readBounded reads up to limit bytes and reports whether the file had more.
func readBounded(path string, limit int) ([]byte, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, false, err
	}
	if len(raw) > limit {
		return raw[:limit], true, nil
	}
	return raw, false, nil
}

// outlineLine is one flattened key-path → scalar-value pair.
type outlineLine struct {
	path  string
	value string
}

func renderOutline(lines []outlineLine) string {
	var b strings.Builder
	for _, l := range lines {
		if l.path == "" {
			b.WriteString(l.value)
		} else {
			b.WriteString(l.path)
			b.WriteString(": ")
			b.WriteString(l.value)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// --- JSON / YAML outlines (shared map/slice walk) --------------------------

func jsonOutline(raw []byte) ([]outlineLine, error) {
	var v any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	// Reject trailing garbage after the first value (a malformed doc that happens
	// to start with a valid value) by requiring EOF.
	if dec.More() {
		return nil, fmt.Errorf("trailing data after json value")
	}
	var lines []outlineLine
	if err := flatten("", v, &lines); err != nil {
		return nil, err
	}
	return lines, nil
}

func yamlOutline(raw []byte) ([]outlineLine, error) {
	var v any
	if err := yaml.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	if v == nil {
		return nil, fmt.Errorf("empty yaml document")
	}
	var lines []outlineLine
	if err := flatten("", v, &lines); err != nil {
		return nil, err
	}
	return lines, nil
}

// flatten walks a decoded JSON/YAML value (maps, slices, scalars) into key-path
// outline lines. Map keys are sorted for stable output (deterministic chunks).
// Returns an error once the outline exceeds maxOutlineLines so a pathological
// document cannot build an unbounded slice.
func flatten(prefix string, v any, out *[]outlineLine) error {
	if len(*out) > maxOutlineLines {
		return fmt.Errorf("outline exceeds %d lines", maxOutlineLines)
	}
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if err := flatten(joinPath(prefix, k), t[k], out); err != nil {
				return err
			}
		}
	case map[any]any: // yaml.v3 with non-string keys
		keys := make([]string, 0, len(t))
		km := make(map[string]any, len(t))
		for k, val := range t {
			ks := fmt.Sprintf("%v", k)
			keys = append(keys, ks)
			km[ks] = val
		}
		sort.Strings(keys)
		for _, k := range keys {
			if err := flatten(joinPath(prefix, k), km[k], out); err != nil {
				return err
			}
		}
	case []any:
		for i, item := range t {
			if err := flatten(fmt.Sprintf("%s[%d]", prefix, i), item, out); err != nil {
				return err
			}
		}
	default:
		*out = append(*out, outlineLine{path: prefix, value: scalarString(t)})
	}
	return nil
}

func joinPath(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

func scalarString(v any) string {
	if v == nil {
		return "null"
	}
	return fmt.Sprintf("%v", v)
}

// --- XML outline -----------------------------------------------------------

// xmlOutline walks the XML token stream into element-path / text outline lines.
// encoding/xml does NOT expand external entities by default — we never set a
// custom dec.Entity map or resolver, so this is XXE-safe (consistent with A4's
// office XML parsing). Attributes are emitted as path@attr lines; element text
// as path lines.
func xmlOutline(raw []byte) ([]outlineLine, error) {
	dec := xml.NewDecoder(bytes.NewReader(raw))
	// Strict by default; no external entity resolution (XXE-safe).
	var lines []outlineLine
	var stack []string
	var cur strings.Builder
	sawElement := false

	flushText := func() {
		text := strings.TrimSpace(cur.String())
		cur.Reset()
		if text == "" {
			return
		}
		path := strings.Join(stack, ".")
		lines = append(lines, outlineLine{path: path, value: text})
	}

	for {
		if len(lines) > maxOutlineLines {
			return nil, fmt.Errorf("xml outline exceeds %d lines", maxOutlineLines)
		}
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			flushText()
			sawElement = true
			stack = append(stack, t.Name.Local)
			for _, a := range t.Attr {
				path := strings.Join(stack, ".") + "@" + a.Name.Local
				lines = append(lines, outlineLine{path: path, value: a.Value})
			}
		case xml.CharData:
			cur.Write(t)
		case xml.EndElement:
			flushText()
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if !sawElement {
		return nil, fmt.Errorf("no xml elements")
	}
	return lines, nil
}
