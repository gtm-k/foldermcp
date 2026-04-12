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
}

func NewEmbedder(modelPath, tokenizerPath string) *Embedder {
	return &Embedder{modelPath: modelPath, tokenizerPath: tokenizerPath}
}

func (e *Embedder) init() error {
	e.once.Do(func() {
		if err := ort.InitializeEnvironment(); err != nil {
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

// Embed runs inference on pre-tokenized inputs (batch=1) and returns
// a 384-dim float32 vector after mean-pooling and L2 normalization.
func (e *Embedder) Embed(ids, mask, tts []int64) ([]float32, error) {
	if err := e.init(); err != nil {
		return nil, err
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
