//go:build cgo

package embed

import (
	"fmt"
	"math"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

const (
	ModelName    = "all-MiniLM-L6-v2"
	ModelVersion = "2.2.0"
	Dimension    = 384
	MaxSeqLen    = 128
)

// ortEnvOnce guards the process-wide ONNX Runtime environment init.
// InitializeEnvironment() errors if called twice in the same process,
// so all Embedder instances share a single init.
var (
	ortEnvOnce sync.Once
	ortEnvErr  error
)

func initOrtEnv() error {
	ortEnvOnce.Do(func() {
		ortEnvErr = ort.InitializeEnvironment()
	})
	return ortEnvErr
}

// Embedder wraps an ONNX Runtime session for all-MiniLM-L6-v2 inference.
// It pre-allocates input/output tensors and reuses them across calls.
// Not goroutine-safe — callers must serialize access (the pipeline is
// single-writer by design).
type Embedder struct {
	once          sync.Once
	session       *ort.AdvancedSession
	inputIDs      *ort.Tensor[int64]
	attentionMask *ort.Tensor[int64]
	tokenTypeIDs  *ort.Tensor[int64]
	output        *ort.Tensor[float32]
	initErr       error
	modelPath     string
	tokenizerPath string

	// tokenizer is parsed once and cached (D28b-E2 follow-up A):
	// EmbedQuery runs once per chunk on the Runner's hot path, and
	// re-reading + re-parsing the ~450 KB tokenizer.json per call would
	// dominate per-chunk cost.
	tokOnce   sync.Once
	tokenizer *Tokenizer
	tokErr    error
}

func NewEmbedder(modelPath, tokenizerPath string) *Embedder {
	return &Embedder{modelPath: modelPath, tokenizerPath: tokenizerPath}
}

// loadTokenizer loads and caches the WordPiece tokenizer. Safe to call
// repeatedly; only the first call reads tokenizer.json.
func (e *Embedder) loadTokenizer() (*Tokenizer, error) {
	e.tokOnce.Do(func() {
		e.tokenizer, e.tokErr = LoadTokenizer(e.tokenizerPath)
	})
	if e.tokErr != nil {
		return nil, fmt.Errorf("load tokenizer: %w", e.tokErr)
	}
	return e.tokenizer, nil
}

func (e *Embedder) init() error {
	e.once.Do(func() {
		// Load the tokenizer first so Init() fails fast on a bad
		// tokenizer path too, not only on a bad model/runtime — the
		// Runner treats Init errors as fatal (pre-mortem Story 1).
		if _, err := e.loadTokenizer(); err != nil {
			e.initErr = err
			return
		}
		if err := initOrtEnv(); err != nil {
			e.initErr = fmt.Errorf("ort init: %w", err)
			return
		}

		var err error
		shape := ort.NewShape(1, MaxSeqLen)

		e.inputIDs, err = ort.NewEmptyTensor[int64](shape)
		if err != nil {
			e.initErr = fmt.Errorf("input_ids tensor: %w", err)
			return
		}
		e.attentionMask, err = ort.NewEmptyTensor[int64](shape)
		if err != nil {
			e.initErr = fmt.Errorf("attention_mask tensor: %w", err)
			return
		}
		e.tokenTypeIDs, err = ort.NewEmptyTensor[int64](shape)
		if err != nil {
			e.initErr = fmt.Errorf("token_type_ids tensor: %w", err)
			return
		}

		// Output: last_hidden_state [1, seq_len, 384]
		outShape := ort.NewShape(1, MaxSeqLen, Dimension)
		e.output, err = ort.NewEmptyTensor[float32](outShape)
		if err != nil {
			e.initErr = fmt.Errorf("output tensor: %w", err)
			return
		}

		inputs := []ort.Value{e.inputIDs, e.attentionMask, e.tokenTypeIDs}
		outputs := []ort.Value{e.output}

		e.session, err = ort.NewAdvancedSession(
			e.modelPath,
			[]string{"input_ids", "attention_mask", "token_type_ids"},
			[]string{"last_hidden_state"},
			inputs, outputs, nil,
		)
		if err != nil {
			e.initErr = fmt.Errorf("new session: %w", err)
			return
		}
	})
	return e.initErr
}

// Init eagerly loads the WordPiece tokenizer and initializes the ONNX
// Runtime session. Exported wrapper around the lazy init() (D28b D10) so
// the pipeline Runner can fail fast on a bad model/tokenizer/runtime
// setup before entering the file loop (pre-mortem Story 1) instead of
// swallowing the same init error on every file via per-file recovery.
func (e *Embedder) Init() error { return e.init() }

// Embed runs inference on pre-tokenized inputs (batch=1) and returns
// a 384-dim float32 vector after mean-pooling and L2 normalization.
// All three input slices must be exactly MaxSeqLen (128) elements.
func (e *Embedder) Embed(ids, mask, tts []int64) ([]float32, error) {
	if err := e.init(); err != nil {
		return nil, err
	}
	if len(ids) != MaxSeqLen || len(mask) != MaxSeqLen || len(tts) != MaxSeqLen {
		return nil, fmt.Errorf("input length mismatch: ids=%d mask=%d tts=%d, all must be %d",
			len(ids), len(mask), len(tts), MaxSeqLen)
	}

	// Copy input data into pre-allocated tensors
	copy(e.inputIDs.GetData(), ids)
	copy(e.attentionMask.GetData(), mask)
	copy(e.tokenTypeIDs.GetData(), tts)

	if err := e.session.Run(); err != nil {
		return nil, fmt.Errorf("ort run: %w", err)
	}

	// Mean-pool: sum hidden states weighted by attention mask, divide by mask sum
	outData := e.output.GetData() // [1 * MaxSeqLen * 384] flat
	pooled := make([]float32, Dimension)
	var maskSum float32
	for s := 0; s < MaxSeqLen; s++ {
		m := float32(mask[s])
		if m == 0 {
			continue
		}
		maskSum += m
		base := s * Dimension
		for d := 0; d < Dimension; d++ {
			pooled[d] += outData[base+d] * m
		}
	}
	if maskSum > 0 {
		for d := range pooled {
			pooled[d] /= maskSum
		}
	}

	// L2 normalize
	var norm float64
	for _, v := range pooled {
		norm += float64(v) * float64(v)
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		for i := range pooled {
			pooled[i] = float32(float64(pooled[i]) / norm)
		}
	}

	return pooled, nil
}

func (e *Embedder) Close() error {
	if e.session != nil {
		_ = e.session.Destroy()
	}
	if e.inputIDs != nil {
		_ = e.inputIDs.Destroy()
	}
	if e.attentionMask != nil {
		_ = e.attentionMask.Destroy()
	}
	if e.tokenTypeIDs != nil {
		_ = e.tokenTypeIDs.Destroy()
	}
	if e.output != nil {
		_ = e.output.Destroy()
	}
	return nil
}
