//go:build cgo

package chunker

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// officeExtractorVersion tags the on-disk provenance of office-extracted text
// (R7: not in the embedding fingerprint, so a future change needs an explicit
// reindex policy). Distinct from the PDF extractor version.
const officeExtractorVersion = "office-zipxml-v1"

// maxOfficeXML caps the document XML we parse from the ZIP, mirroring the
// runner's per-file read cap intent — a malicious or corrupt container should
// not be able to make us allocate unbounded memory.
const maxOfficeXML = 64 * 1024 * 1024 // 64 MB of document XML

// ChunkOffice extracts text from a docx or odt ZIP container and produces
// paragraph-anchored prose chunks with chunk_kind='office_text' (DISTINCT from
// pdf_text — provenance must not be overloaded). title is used in the chunk
// header; pass the document base name. ext is the lowercased extension
// (".docx" or ".odt") and selects the document part + paragraph element.
//
// docx → word/document.xml, paragraphs on <w:p>, text in <w:t> runs.
// odt  → content.xml,       paragraphs on <text:p>/<text:h> (their character
//
//	data, recursively).
//
// The extracted text is a single synthetic "page" (office formats have no
// pdftotext-style page breaks), so headers are "p1¶<para> — <title>" and the
// page-anchored chunking core is reused with no form-feed. byte_start/byte_end
// are relative to the joined extracted text (D18).
func ChunkOffice(path, title, ext string, cfg Config, counter Counter) ([]ExtractedChunk, ExtractStats, error) {
	paras, err := extractOfficeParagraphs(path, ext)
	if err != nil {
		return nil, ExtractStats{}, err
	}
	// Join paragraphs with a blank line so the shared core's paragraph splitter
	// (which splits on "\n\n") reproduces the paragraph boundaries, and byte
	// offsets remain relative to this joined text.
	text := strings.Join(paras, "\n\n")
	return chunkExtractedPages(text, title, officeExtractorVersion, cfg, counter, false)
}

// extractOfficeParagraphs opens the ZIP container and returns the document's
// paragraphs as plain-text strings (empty paragraphs dropped). It never panics
// on a malformed container — it returns an error the runner records visibly.
func extractOfficeParagraphs(path, ext string) ([]string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("open office container %s: %w", path, err)
	}
	defer func() { _ = zr.Close() }()

	var docPart string
	switch ext {
	case ".docx":
		docPart = "word/document.xml"
	case ".odt":
		docPart = "content.xml"
	default:
		return nil, fmt.Errorf("unsupported office extension %q", ext)
	}

	var f *zip.File
	for _, zf := range zr.File {
		if zf.Name == docPart {
			f = zf
			break
		}
	}
	if f == nil {
		return nil, fmt.Errorf("office container %s missing %s", path, docPart)
	}

	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("open %s in %s: %w", docPart, path, err)
	}
	defer func() { _ = rc.Close() }()

	raw, err := io.ReadAll(io.LimitReader(rc, maxOfficeXML))
	if err != nil {
		return nil, fmt.Errorf("read %s in %s: %w", docPart, path, err)
	}

	if ext == ".docx" {
		return parseDocxParagraphs(raw)
	}
	return parseOdtParagraphs(raw)
}

// parseDocxParagraphs walks the WordprocessingML stream: paragraph boundaries
// are <w:p> elements; visible text lives in <w:t> runs. Namespace-agnostic
// (matches on Local names) so it tolerates the various w:/wpc: prefixes real
// writers emit. <w:tab>/<w:br> are treated as whitespace within a paragraph.
func parseDocxParagraphs(raw []byte) ([]string, error) {
	dec := xml.NewDecoder(strings.NewReader(string(raw)))
	var paras []string
	var cur strings.Builder
	inText := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("docx xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inText = true
			case "tab":
				cur.WriteByte('\t')
			case "br", "cr":
				cur.WriteByte(' ')
			}
		case xml.CharData:
			if inText {
				cur.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				paras = appendNonEmpty(paras, cur.String())
				cur.Reset()
			}
		}
	}
	// Flush a trailing paragraph that had no closing </w:p> (defensive).
	paras = appendNonEmpty(paras, cur.String())
	return paras, nil
}

// parseOdtParagraphs walks the OpenDocument content stream: paragraph and
// heading boundaries are <text:p> / <text:h> (Local names "p"/"h"); their
// character data is the text. Nested spans (<text:span>) contribute their
// CharData transparently because we accumulate all CharData while inside a
// paragraph element.
func parseOdtParagraphs(raw []byte) ([]string, error) {
	dec := xml.NewDecoder(strings.NewReader(string(raw)))
	var paras []string
	var cur strings.Builder
	depth := 0 // >0 while inside a text:p / text:h
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("odt xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if isOdtParagraph(t) {
				depth++
			} else if depth > 0 && (t.Name.Local == "tab" || t.Name.Local == "s") {
				cur.WriteByte(' ')
			} else if depth > 0 && t.Name.Local == "line-break" {
				cur.WriteByte(' ')
			}
		case xml.CharData:
			if depth > 0 {
				cur.Write(t)
			}
		case xml.EndElement:
			if isOdtParagraphEnd(t) && depth > 0 {
				depth--
				if depth == 0 {
					paras = appendNonEmpty(paras, cur.String())
					cur.Reset()
				}
			}
		}
	}
	paras = appendNonEmpty(paras, cur.String())
	return paras, nil
}

func isOdtParagraph(t xml.StartElement) bool {
	return t.Name.Local == "p" || t.Name.Local == "h"
}

func isOdtParagraphEnd(t xml.EndElement) bool {
	return t.Name.Local == "p" || t.Name.Local == "h"
}

func appendNonEmpty(paras []string, s string) []string {
	if strings.TrimSpace(s) != "" {
		paras = append(paras, strings.TrimSpace(s))
	}
	return paras
}

// --- minimal-container builders (shared by chunker + pipeline tests) ---------
//
// These live in the production file (not a _test.go) so other packages' tests
// (pipeline/runner_test.go, R2) can build the same real, NUL-bearing fixtures
// without duplicating the ZIP plumbing. They take no testing dependency. A real
// Deflate-compressed ZIP reliably carries NUL bytes in its first 8 KB, which is
// the exact condition R2 asserts the binary detector must NOT misclassify.

func officeXMLEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// BuildDOCX returns the bytes of a minimal but valid .docx (ZIP with
// [Content_Types].xml + _rels/.rels + word/document.xml), one <w:p> per
// paragraph.
func BuildDOCX(paras []string) ([]byte, error) {
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	body.WriteString(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	for _, p := range paras {
		body.WriteString(`<w:p><w:r><w:t xml:space="preserve">`)
		body.WriteString(officeXMLEscape(p))
		body.WriteString(`</w:t></w:r></w:p>`)
	}
	body.WriteString(`</w:body></w:document>`)

	files := []zipEntry{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
			`</Types>`, zip.Deflate},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>` +
			`</Relationships>`, zip.Deflate},
		{"word/document.xml", body.String(), zip.Deflate},
	}
	return buildZip(files)
}

// BuildODT returns the bytes of a minimal but valid .odt (ZIP with a stored
// mimetype entry first + content.xml), one <text:p> per paragraph.
func BuildODT(paras []string) ([]byte, error) {
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	body.WriteString(`<office:document-content xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0"><office:body><office:text>`)
	for _, p := range paras {
		body.WriteString(`<text:p>`)
		body.WriteString(officeXMLEscape(p))
		body.WriteString(`</text:p>`)
	}
	body.WriteString(`</office:text></office:body></office:document-content>`)

	files := []zipEntry{
		{"mimetype", "application/vnd.oasis.opendocument.text", zip.Store},
		{"content.xml", body.String(), zip.Deflate},
	}
	return buildZip(files)
}

type zipEntry struct {
	name    string
	content string
	method  uint16
}

func buildZip(entries []zipEntry) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: e.name, Method: e.method})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(e.content)); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
