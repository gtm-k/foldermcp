//go:build cgo

package embed

import "math"

// QuantizeInt8 maps a float32 vector to int8 using symmetric per-vector scaling.
// The scale is the max absolute value; int8 values are rounded to nearest.
// Retrieval quality degrades ~1-2% per spec §A.2.
func QuantizeInt8(v []float32) []int8 {
	var maxAbs float32
	for _, x := range v {
		if a := float32(math.Abs(float64(x))); a > maxAbs {
			maxAbs = a
		}
	}
	if maxAbs == 0 {
		return make([]int8, len(v))
	}
	scale := 127.0 / maxAbs
	out := make([]int8, len(v))
	for i, x := range v {
		q := float32(math.Round(float64(x * scale)))
		if q > 127 {
			q = 127
		} else if q < -128 {
			q = -128
		}
		out[i] = int8(q)
	}
	return out
}
