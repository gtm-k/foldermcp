//go:build cgo

package embed

import (
	"math"
	"testing"
)

func TestQuantizeInt8PreservesShape(t *testing.T) {
	v := []float32{0.1, -0.2, 0.3, 0.4, -0.5}
	q := QuantizeInt8(v)
	if len(q) != len(v) {
		t.Errorf("len = %d, want %d", len(q), len(v))
	}
	// Peak value (-0.5) should map to -127
	if q[4] != -127 {
		t.Errorf("max-abs entry q[4] = %d, want -127", q[4])
	}
}

func TestQuantizeZero(t *testing.T) {
	q := QuantizeInt8([]float32{0, 0, 0})
	for _, x := range q {
		if x != 0 {
			t.Errorf("zero vector → %d", x)
		}
	}
}

func TestQuantizeSymmetric(t *testing.T) {
	v := []float32{1.0, -1.0, 0.5, -0.5}
	q := QuantizeInt8(v)
	if q[0] != 127 {
		t.Errorf("q[0] = %d, want 127", q[0])
	}
	if q[1] != -127 {
		t.Errorf("q[1] = %d, want -127", q[1])
	}
	expected := int8(math.Round(0.5 * 127))
	if q[2] != expected {
		t.Errorf("q[2] = %d, want %d", q[2], expected)
	}
}
