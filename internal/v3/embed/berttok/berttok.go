// Package berttok is a pure-Go BERT WordPiece tokenizer that reproduces the
// HuggingFace tokenizer.json pipeline (added-token matching + BertNormalizer +
// BertPreTokenizer + WordPiece + BERT post-processing) byte-for-byte. It is
// intentionally cgo-free so it builds in the no-cgo matrix and is unit-testable
// natively; the cgo embed package wraps it. Parity is locked by
// testdata/bert_golden.json, generated from the real Python HF tokenizer.
package berttok

import (
	"encoding/json"
	"os"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// addedTok is a special token matched against raw input before normalization
// (e.g. [CLS], [SEP], [MASK]) — HF AddedVocabulary semantics.
type addedTok struct {
	content string
	id      int64
}

// Tokenizer encodes text to (input_ids, attention_mask, token_type_ids), each
// of length maxSeqLen, matching the HF BERT tokenizer for the loaded vocab.
type Tokenizer struct {
	vocab        map[string]int64
	added        []addedTok // special tokens, matched on raw text (not normalized)
	clsID        int64
	sepID        int64
	unkID        int64
	padID        int64
	contPrefix   string
	maxInputChar int // per word, in runes
	maxSeqLen    int

	cleanText     bool
	handleChinese bool
	stripAccents  bool
	lowercase     bool
}

// tokenizerJSON is the subset of HuggingFace tokenizer.json we read.
type tokenizerJSON struct {
	Normalizer struct {
		CleanText     *bool `json:"clean_text"`
		HandleChinese *bool `json:"handle_chinese_chars"`
		StripAccents  *bool `json:"strip_accents"` // null -> resolves to lowercase
		Lowercase     *bool `json:"lowercase"`
	} `json:"normalizer"`
	Model struct {
		UnkToken         string           `json:"unk_token"`
		ContinuingPrefix string           `json:"continuing_subword_prefix"`
		MaxInputChars    int              `json:"max_input_chars_per_word"`
		Vocab            map[string]int64 `json:"vocab"`
	} `json:"model"`
	AddedTokens []struct {
		ID         int64  `json:"id"`
		Content    string `json:"content"`
		Special    bool   `json:"special"`
		Normalized bool   `json:"normalized"`
	} `json:"added_tokens"`
}

func boolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

// New loads a tokenizer from a HuggingFace tokenizer.json. maxSeqLen is the
// fixed output length (e.g. 128) the model expects.
func New(tokenizerJSONPath string, maxSeqLen int) (*Tokenizer, error) {
	raw, err := os.ReadFile(tokenizerJSONPath)
	if err != nil {
		return nil, err
	}
	var hf tokenizerJSON
	if err := json.Unmarshal(raw, &hf); err != nil {
		return nil, err
	}
	lower := boolOr(hf.Normalizer.Lowercase, true)
	contPrefix := hf.Model.ContinuingPrefix
	if contPrefix == "" {
		contPrefix = "##"
	}
	maxChars := hf.Model.MaxInputChars
	if maxChars == 0 {
		maxChars = 100
	}
	unk := hf.Model.UnkToken
	if unk == "" {
		unk = "[UNK]"
	}
	t := &Tokenizer{
		vocab:         hf.Model.Vocab,
		clsID:         hf.Model.Vocab["[CLS]"],
		sepID:         hf.Model.Vocab["[SEP]"],
		unkID:         hf.Model.Vocab[unk],
		padID:         hf.Model.Vocab["[PAD]"],
		contPrefix:    contPrefix,
		maxInputChar:  maxChars,
		maxSeqLen:     maxSeqLen,
		cleanText:     boolOr(hf.Normalizer.CleanText, true),
		handleChinese: boolOr(hf.Normalizer.HandleChinese, true),
		// strip_accents == null resolves to the lowercase flag (HF semantics).
		stripAccents: boolOr(hf.Normalizer.StripAccents, lower),
		lowercase:    lower,
	}
	// Non-normalized special tokens are matched on the raw input (HF).
	for _, a := range hf.AddedTokens {
		if a.Special && !a.Normalized && a.Content != "" {
			t.added = append(t.added, addedTok{content: a.Content, id: a.ID})
		}
	}
	return t, nil
}

// Encode reproduces HF: split on special tokens -> per gap (normalize ->
// basic-tokenize -> WordPiece) -> [CLS]..[SEP] -> truncate to maxSeqLen
// (reserving the two special tokens) -> pad.
func (t *Tokenizer) Encode(text string) (ids, mask, typeIDs []int64) {
	var content []int64
	for _, seg := range t.splitOnAddedTokens(text) {
		if seg.special {
			content = append(content, seg.id)
			continue
		}
		for _, word := range basicTokenize(t.normalize(seg.text)) {
			content = append(content, t.wordPiece(word)...)
		}
	}
	if max := t.maxSeqLen - 2; len(content) > max {
		content = content[:max]
	}

	ids = make([]int64, t.maxSeqLen)
	mask = make([]int64, t.maxSeqLen)
	typeIDs = make([]int64, t.maxSeqLen) // single sequence -> all zeros
	for i := range ids {
		switch {
		case i == 0:
			ids[i] = t.clsID
			mask[i] = 1
		case i <= len(content):
			ids[i] = content[i-1]
			mask[i] = 1
		case i == len(content)+1:
			ids[i] = t.sepID
			mask[i] = 1
		default:
			ids[i] = t.padID
		}
	}
	return ids, mask, typeIDs
}

// ── added-token matching (leftmost; tokens don't overlap for BERT) ──

type segment struct {
	special bool
	id      int64
	text    string
}

func (t *Tokenizer) splitOnAddedTokens(text string) []segment {
	if len(t.added) == 0 {
		return []segment{{text: text}}
	}
	var segs []segment
	for text != "" {
		bestIdx, bestLen := -1, 0
		var bestID int64
		for _, a := range t.added {
			idx := strings.Index(text, a.content)
			if idx < 0 {
				continue
			}
			if bestIdx == -1 || idx < bestIdx || (idx == bestIdx && len(a.content) > bestLen) {
				bestIdx, bestLen, bestID = idx, len(a.content), a.id
			}
		}
		if bestIdx == -1 {
			segs = append(segs, segment{text: text})
			break
		}
		if bestIdx > 0 {
			segs = append(segs, segment{text: text[:bestIdx]})
		}
		segs = append(segs, segment{special: true, id: bestID})
		text = text[bestIdx+bestLen:]
	}
	return segs
}

// ── BertNormalizer ───────────────────────────────────────

func (t *Tokenizer) normalize(s string) string {
	if t.cleanText {
		s = cleanText(s)
	}
	if t.handleChinese {
		s = handleChineseChars(s)
	}
	if t.stripAccents {
		s = stripAccents(s)
	}
	if t.lowercase {
		s = strings.ToLower(s)
	}
	return s
}

// cleanText drops null/replacement/control characters and collapses any
// whitespace rune to a single space (HF _clean_text).
func cleanText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == 0 || r == 0xFFFD || isControl(r) {
			continue
		}
		if isWhitespace(r) {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// handleChineseChars surrounds every CJK character with spaces so it becomes
// its own token (HF _tokenize_chinese_chars).
func handleChineseChars(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if isChineseChar(r) {
			b.WriteByte(' ')
			b.WriteRune(r)
			b.WriteByte(' ')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// stripAccents NFD-decomposes and drops nonspacing combining marks (HF
// strip_accents). For this model strip_accents resolves to the lowercase flag.
func stripAccents(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range norm.NFD.String(s) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ── BertPreTokenizer ─────────────────────────────────────

// basicTokenize splits on whitespace, then isolates each punctuation char
// into its own token (HF BertPreTokenizer / BasicTokenizer).
func basicTokenize(s string) []string {
	var out []string
	for _, word := range strings.Fields(s) {
		out = append(out, splitOnPunct(word)...)
	}
	return out
}

func splitOnPunct(word string) []string {
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			out = append(out, string(cur))
			cur = cur[:0]
		}
	}
	for _, r := range word {
		if isPunctuation(r) {
			flush()
			out = append(out, string(r))
		} else {
			cur = append(cur, r)
		}
	}
	flush()
	return out
}

// ── WordPiece ────────────────────────────────────────────

func (t *Tokenizer) wordPiece(token string) []int64 {
	runes := []rune(token)
	if len(runes) > t.maxInputChar {
		return []int64{t.unkID}
	}
	var out []int64
	start := 0
	for start < len(runes) {
		end := len(runes)
		var id int64
		found := false
		for start < end {
			sub := string(runes[start:end])
			if start > 0 {
				sub = t.contPrefix + sub
			}
			if v, ok := t.vocab[sub]; ok {
				id = v
				found = true
				break
			}
			end--
		}
		if !found {
			return []int64{t.unkID} // whole token is [UNK] if any piece misses
		}
		out = append(out, id)
		start = end
	}
	return out
}

// ── character classes (HF parity) ────────────────────────

// isControl matches HF _is_control: category C* except \t \n \r.
func isControl(r rune) bool {
	if r == '\t' || r == '\n' || r == '\r' {
		return false
	}
	return unicode.In(r, unicode.C)
}

// isWhitespace matches HF _is_whitespace: space/tab/newline/CR or category Zs.
func isWhitespace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r':
		return true
	}
	return unicode.Is(unicode.Zs, r)
}

// isPunctuation matches HF _is_punctuation: the ASCII symbol ranges (which
// include $ + = ~ ^ ` | < > that Unicode classes as symbols, NOT punctuation)
// PLUS any Unicode punctuation category.
func isPunctuation(r rune) bool {
	if (r >= 33 && r <= 47) || (r >= 58 && r <= 64) ||
		(r >= 91 && r <= 96) || (r >= 123 && r <= 126) {
		return true
	}
	return unicode.IsPunct(r)
}

// isChineseChar matches HF _is_chinese_char: the 8 CJK Unicode blocks.
func isChineseChar(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) ||
		(r >= 0x3400 && r <= 0x4DBF) ||
		(r >= 0x20000 && r <= 0x2A6DF) ||
		(r >= 0x2A700 && r <= 0x2B73F) ||
		(r >= 0x2B740 && r <= 0x2B81F) ||
		// HF Rust `tokenizers` starts this block at 0x2B920 (NOT 0x2B820 as in
		// Google/HF-Python BERT) — verified against the model's tokenizer.json
		// oracle. The 0.39-recall parity target is the Rust lib, so match it.
		(r >= 0x2B920 && r <= 0x2CEAF) ||
		(r >= 0xF900 && r <= 0xFAFF) ||
		(r >= 0x2F800 && r <= 0x2FA1F)
}
