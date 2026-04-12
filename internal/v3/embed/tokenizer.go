//go:build cgo

package embed

import (
	"encoding/json"
	"os"
	"strings"
)

// Tokenizer is a minimal BERT WordPiece tokenizer loaded from the HuggingFace
// tokenizer.json shipped with all-MiniLM-L6-v2. It supports Encode(text)
// returning (input_ids, attention_mask, token_type_ids) padded/truncated
// to MaxSeqLen=128.
type Tokenizer struct {
	vocab     map[string]int64
	clsID     int64
	sepID     int64
	unkID     int64
	padID     int64
	maxSeqLen int
}

func LoadTokenizer(path string) (*Tokenizer, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var hf struct {
		Model struct {
			Vocab map[string]int64 `json:"vocab"`
		} `json:"model"`
	}
	if err := json.Unmarshal(raw, &hf); err != nil {
		return nil, err
	}
	t := &Tokenizer{
		vocab:     hf.Model.Vocab,
		maxSeqLen: MaxSeqLen,
	}
	t.clsID = t.vocab["[CLS]"]
	t.sepID = t.vocab["[SEP]"]
	t.unkID = t.vocab["[UNK]"]
	t.padID = t.vocab["[PAD]"]
	return t, nil
}

// Encode returns (input_ids, attention_mask, token_type_ids) each of length
// MaxSeqLen. Pads with padID; truncates tail if text is longer than MaxSeqLen-2.
func (t *Tokenizer) Encode(text string) ([]int64, []int64, []int64) {
	text = strings.ToLower(text)
	words := strings.Fields(text)

	ids := []int64{t.clsID}
	for _, w := range words {
		if id, ok := t.vocab[w]; ok {
			ids = append(ids, id)
		} else {
			// WordPiece: try progressively shorter prefixes with ## continuation
			ids = append(ids, t.wordPiece(w)...)
		}
		if len(ids) >= t.maxSeqLen-1 {
			break
		}
	}
	if len(ids) > t.maxSeqLen-1 {
		ids = ids[:t.maxSeqLen-1]
	}
	ids = append(ids, t.sepID)

	inputIDs := make([]int64, t.maxSeqLen)
	mask := make([]int64, t.maxSeqLen)
	tts := make([]int64, t.maxSeqLen)
	for i := 0; i < t.maxSeqLen; i++ {
		if i < len(ids) {
			inputIDs[i] = ids[i]
			mask[i] = 1
		} else {
			inputIDs[i] = t.padID
		}
	}
	return inputIDs, mask, tts
}

func (t *Tokenizer) wordPiece(word string) []int64 {
	var out []int64
	start := 0
	for start < len(word) {
		end := len(word)
		var id int64
		found := false
		for end > start {
			sub := word[start:end]
			if start > 0 {
				sub = "##" + sub
			}
			if v, ok := t.vocab[sub]; ok {
				id = v
				found = true
				break
			}
			end--
		}
		if !found {
			return []int64{t.unkID}
		}
		out = append(out, id)
		start = end
	}
	return out
}
