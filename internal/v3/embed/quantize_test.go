package embed

import (
	"math"
	"math/rand"
	"testing"
)

// Under the FIXED global scale, a given float component must quantize to the
// SAME int8 code regardless of the other components in the vector. Per-vector
// scaling (scale = 127/maxAbs) violated this — each vector was divided by its
// own max-abs — which broke cross-vector L2 comparability and was the root
// cause of low semantic recall. This is the single test that fails under the
// old per-vector code (0.2 → 127 vs 28) and passes under fixed scale.
func TestFixedScaleIsUniformAcrossVectors(t *testing.T) {
	a := QuantizeInt8([]float32{0.2, 0.1, 0.0, 0.0})
	b := QuantizeInt8([]float32{0.2, 0.9, -0.3, 0.4})
	if a[0] != b[0] {
		t.Fatalf("component 0.2 quantized to %d in vector a but %d in vector b; "+
			"a fixed scale must map equal inputs to equal codes", a[0], b[0])
	}
}

// Zero vectors map to all-zero codes (preserved from prior behavior).
func TestQuantizeZero(t *testing.T) {
	for _, x := range QuantizeInt8([]float32{0, 0, 0}) {
		if x != 0 {
			t.Errorf("zero vector → %d, want 0", x)
		}
	}
}

// Output length always matches input length.
func TestQuantizePreservesShape(t *testing.T) {
	v := []float32{0.1, -0.2, 0.3, 0.4, -0.5}
	if got := len(QuantizeInt8(v)); got != len(v) {
		t.Errorf("len = %d, want %d", got, len(v))
	}
}

// Components beyond the representable range saturate symmetrically to ±127.
func TestFixedScaleClampsSymmetric(t *testing.T) {
	q := QuantizeInt8([]float32{9.0, -9.0})
	if q[0] != 127 {
		t.Errorf("large positive saturates to %d, want 127", q[0])
	}
	if q[1] != -127 {
		t.Errorf("large negative saturates to %d, want -127", q[1])
	}
}

// Formula + scale pin. These exact codes lock both the q = round(x·Int8Scale)
// formula and the value of Int8Scale (256): if anyone changes the scale, this
// fails loudly in CI — the codes on disk would silently change otherwise. Note
// 0.5·256 = 128 saturates to 127, and -0.5·256 = -128 saturates to -127.
func TestQuantizeFormulaAtScale256(t *testing.T) {
	if Int8Scale != 256.0 {
		t.Fatalf("Int8Scale = %v; this golden test is pinned to 256 — update the "+
			"expected codes below and bump QuantizationModeString consumers", Int8Scale)
	}
	in := []float32{0.1, 0.25, 0.5, -0.5, 1.0, -1.0}
	want := []int8{26, 64, 127, -127, 127, -127}
	got := QuantizeInt8(in)
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("QuantizeInt8(%v)[%d] = %d, want %d", in[i], i, got[i], want[i])
		}
	}
}

// Saturation band pin: with Int8Scale=256 the clip threshold is 127.5/256 ≈
// 0.498. A component below it keeps its magnitude (not saturated); a component
// above it saturates to 127. This is the regime where the L2≈cosine identity
// starts to break, so the boundary is worth locking.
func TestClipThresholdBoundary(t *testing.T) {
	q := QuantizeInt8([]float32{0.45, 0.60})
	if q[0] != 115 { // 0.45·256 = 115.2 → 115, well below saturation
		t.Errorf("sub-threshold 0.45 → %d, want 115 (must NOT saturate)", q[0])
	}
	if q[1] != 127 { // 0.60·256 = 153.6 → saturates
		t.Errorf("supra-threshold 0.60 → %d, want 127 (must saturate)", q[1])
	}
}

// The scale-parameterized seam is the reason quantize.go has no cgo tag and an
// exported-but-internal helper: it lets the math be exercised at arbitrary
// scales natively. Without a caller varying the scale, the seam is dead weight.
func TestQuantizeInt8ScaleSeam(t *testing.T) {
	cases := []struct {
		x     float32
		scale float32
		want  int8
	}{
		{0.5, 100, 50},  // round(50)
		{0.5, 254, 127}, // round(127), exactly at the rail, not clamped
		{0.5, 300, 127}, // round(150) → saturates
		{-0.5, 300, -127},
	}
	for _, c := range cases {
		if got := quantizeInt8([]float32{c.x}, c.scale)[0]; got != c.want {
			t.Errorf("quantizeInt8(%v, scale=%v) = %d, want %d", c.x, c.scale, got, c.want)
		}
	}
}

// The recall-critical guarantee, as a population property rather than a single
// hand-picked triple: over many random unit vectors, whenever vector b is
// clearly closer to a than c is BY COSINE (margin > 0.1), the int8 L2 distance
// must agree (d(a,b) < d(a,c)). A faithful fixed scale yields ~zero violations;
// the OLD per-vector scheme (which divided each vector by its own max-abs) and
// a badly-saturating scale both produce many. This is what actually pins the
// fix, and it has discriminating power against a poor Int8Scale.
func TestFixedScaleRankingMatchesCosineOverPopulation(t *testing.T) {
	const (
		dim    = 64
		n      = 64
		margin = 0.10
	)
	rng := rand.New(rand.NewSource(1))

	unit := func() []float32 {
		v := make([]float32, dim)
		var norm float64
		for i := range v {
			x := rng.NormFloat64()
			v[i] = float32(x)
			norm += x * x
		}
		norm = math.Sqrt(norm)
		for i := range v {
			v[i] = float32(float64(v[i]) / norm)
		}
		return v
	}
	cos := func(a, b []float32) float64 { // unit vectors → dot product
		var s float64
		for i := range a {
			s += float64(a[i]) * float64(b[i])
		}
		return s
	}
	l2 := func(p, q []int8) float64 {
		var s float64
		for i := range p {
			d := float64(p[i]) - float64(q[i])
			s += d * d
		}
		return s
	}

	vecs := make([][]float32, n)
	codes := make([][]int8, n)
	for i := range vecs {
		vecs[i] = unit()
		codes[i] = QuantizeInt8(vecs[i])
	}

	var considered, violations int
	for a := 0; a < n; a++ {
		for b := 0; b < n; b++ {
			for c := 0; c < n; c++ {
				if a == b || a == c || b == c {
					continue
				}
				cb, cc := cos(vecs[a], vecs[b]), cos(vecs[a], vecs[c])
				if cb-cc <= margin { // only judge clearly-ordered pairs
					continue
				}
				considered++
				if l2(codes[a], codes[b]) >= l2(codes[a], codes[c]) {
					violations++ // b is closer by cosine but not by int8 L2
				}
			}
		}
	}
	if considered < 1000 {
		t.Fatalf("only %d clearly-ordered triples; population too degenerate to test", considered)
	}
	rate := float64(violations) / float64(considered)
	if rate > 0.005 {
		t.Errorf("int8 L2 ranking disagrees with cosine on %d/%d (%.3f%%) clearly-ordered triples; "+
			"want < 0.5%% — fixed scale is not preserving cosine geometry", violations, considered, rate*100)
	}
	t.Logf("cosine↔int8-L2 agreement: %d/%d ordered triples, %d violations (%.4f%%)",
		considered, considered, violations, rate*100)
}

// QuantizationModeString embeds Int8Scale so a scale change self-invalidates
// indexes. Pin the format so the fingerprint label can't silently drift from
// the scale.
func TestQuantizationModeStringEncodesScale(t *testing.T) {
	if got := QuantizationModeString(); got != "int8_fixed_s256" {
		t.Errorf("QuantizationModeString() = %q, want int8_fixed_s256", got)
	}
}
