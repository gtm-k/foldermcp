//go:build cgo

package chunker

import "testing"

func TestTiktokenCounterNonzero(t *testing.T) {
	c := NewTiktokenCounter()
	n := c.Count("The quick brown fox jumps over the lazy dog.")
	if n < 5 {
		t.Errorf("count = %d, expected ≥5", n)
	}
}

func TestTiktokenCounterEmpty(t *testing.T) {
	c := NewTiktokenCounter()
	n := c.Count("")
	if n != 0 {
		t.Errorf("empty string count = %d, want 0", n)
	}
}

func TestTiktokenCounterImplementsCounter(t *testing.T) {
	var _ Counter = NewTiktokenCounter()
}
