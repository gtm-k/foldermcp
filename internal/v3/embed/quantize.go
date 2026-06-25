package embed

import (
	"fmt"
	"math"
)

// Int8Scale is the FIXED global factor applied to every embedding component
// before int8 quantization. Inputs are L2-normalized unit vectors, so with a
// single shared scale every code lands at radius ~Int8Scale·‖x‖ on a common
// sphere; in the unclipped, fine-rounding regime this makes sqlite-vec's
// raw-int8 L2 KNN a faithful proxy for cosine ranking
// (‖q_a−q_b‖² = 2·Int8Scale²·(1−cosθ)).
//
// That identity is exact ONLY without rounding and without clipping — both of
// which this quantizer applies. recall@10 IS sensitive to the value: too small
// and rounding noise dominates; too large and the most discriminative
// components saturate at ±127 and invert neighbours. The right scale keeps the
// corpus's true max component below the clip threshold (127.5/Int8Scale) while
// using as much of the ±127 range as possible.
//
// 256.0 is an UNVALIDATED conservative default, not a measured optimum: the
// clip threshold is 127.5/256 ≈ 0.498, comfortably above all-MiniLM-L6-v2's
// typical max component (~0.15–0.30), so it should rarely clip — but no harness
// sweep has been run and recorded. Calibration against recall@10 (sweeping
// values ≥256, where range use improves) is a pending follow-up before release.
//
// NOTE: the scale is encoded into the fingerprint via QuantizationModeString,
// so changing Int8Scale self-invalidates existing indexes (they get rebuilt).
const Int8Scale float32 = 256.0

// QuantizationModeString is the embedding_fingerprint label for the current
// int8 scheme. It embeds Int8Scale so that any change to the scale produces a
// different label, which fails the startup fingerprint check and forces a
// rebuild — preventing new query codes from being compared against stored
// codes quantized at a different scale.
func QuantizationModeString() string {
	return fmt.Sprintf("int8_fixed_s%d", int(Int8Scale))
}

// QuantizeInt8 maps a float32 vector to int8 using the fixed global Int8Scale.
// Unlike the prior per-vector scheme (scale = 127/maxAbs), the same input
// component always maps to the same code regardless of the rest of the vector,
// preserving cross-vector comparability.
func QuantizeInt8(v []float32) []int8 {
	return quantizeInt8(v, Int8Scale)
}

// quantizeInt8 is the scale-parameterized core, separated so the math is
// exercised at arbitrary scales in tests without the cgo/ONNX boundary.
func quantizeInt8(v []float32, scale float32) []int8 {
	out := make([]int8, len(v))
	for i, x := range v {
		q := int32(math.Round(float64(x * scale)))
		// Symmetric saturation to the int8 range [-127, 127].
		if q > 127 {
			q = 127
		} else if q < -127 {
			q = -127
		}
		out[i] = int8(q)
	}
	return out
}
