//go:build harness

package main

import (
	"math"
	"testing"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
)

func TestRecallAtK(t *testing.T) {
	judgments := []Judgment{
		{Path: "code-python/session_manager.py", Grade: 2},
		{Path: "code-go/config.go", Grade: 1},
		{Path: "docs-markdown/architecture.md", Grade: 2},
	}

	tests := []struct {
		name     string
		hits     []*pb.SearchHit
		k        int
		wantMin  float64
		wantMax  float64
	}{
		{
			name: "all relevant found",
			hits: []*pb.SearchHit{
				{Path: "/workspace/code-python/session_manager.py", Score: 0.9},
				{Path: "/workspace/code-go/config.go", Score: 0.8},
				{Path: "/workspace/docs-markdown/architecture.md", Score: 0.7},
				{Path: "/workspace/unrelated.txt", Score: 0.6},
			},
			k:       10,
			wantMin: 0.999,
			wantMax: 1.001,
		},
		{
			name: "one of three found",
			hits: []*pb.SearchHit{
				{Path: "/workspace/code-python/session_manager.py", Score: 0.9},
				{Path: "/workspace/unrelated.txt", Score: 0.8},
			},
			k:       10,
			wantMin: 0.333,
			wantMax: 0.334,
		},
		{
			name: "none found",
			hits: []*pb.SearchHit{
				{Path: "/workspace/unrelated.txt", Score: 0.9},
			},
			k:       10,
			wantMin: 0.0,
			wantMax: 0.001,
		},
		{
			name:    "empty hits",
			hits:    nil,
			k:       10,
			wantMin: 0.0,
			wantMax: 0.001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := recallAtK(tt.hits, judgments, tt.k)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("recallAtK() = %f, want [%f, %f]", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestNdcgAtK(t *testing.T) {
	judgments := []Judgment{
		{Path: "code-python/session_manager.py", Grade: 2},
		{Path: "code-go/config.go", Grade: 1},
		{Path: "docs-markdown/architecture.md", Grade: 0}, // not relevant
	}

	t.Run("perfect ranking", func(t *testing.T) {
		// Ideal order: grade-2 first, then grade-1
		hits := []*pb.SearchHit{
			{Path: "/workspace/code-python/session_manager.py", Score: 0.9},
			{Path: "/workspace/code-go/config.go", Score: 0.8},
		}
		got := ndcgAtK(hits, judgments, 10)
		// Perfect ranking should give NDCG = 1.0
		if math.Abs(got-1.0) > 0.001 {
			t.Errorf("ndcgAtK() = %f, want ~1.0 for perfect ranking", got)
		}
	})

	t.Run("reversed ranking", func(t *testing.T) {
		// Suboptimal: grade-1 before grade-2
		hits := []*pb.SearchHit{
			{Path: "/workspace/code-go/config.go", Score: 0.9},         // grade 1
			{Path: "/workspace/code-python/session_manager.py", Score: 0.8}, // grade 2
		}
		got := ndcgAtK(hits, judgments, 10)
		// Should be less than 1.0 but still positive
		if got >= 1.0 || got <= 0.0 {
			t.Errorf("ndcgAtK() = %f, want (0, 1) for reversed ranking", got)
		}
	})

	t.Run("no relevant hits", func(t *testing.T) {
		hits := []*pb.SearchHit{
			{Path: "/workspace/unrelated.txt", Score: 0.9},
		}
		got := ndcgAtK(hits, judgments, 10)
		if got != 0.0 {
			t.Errorf("ndcgAtK() = %f, want 0.0 for no relevant hits", got)
		}
	})

	t.Run("no judgments with grade > 0", func(t *testing.T) {
		zeroJudgments := []Judgment{
			{Path: "foo.py", Grade: 0},
		}
		hits := []*pb.SearchHit{
			{Path: "/workspace/foo.py", Score: 0.9},
		}
		got := ndcgAtK(hits, zeroJudgments, 10)
		// IDCG is 0, so NDCG should be 0
		if got != 0.0 {
			t.Errorf("ndcgAtK() = %f, want 0.0 when all judgments are grade 0", got)
		}
	})
}

func TestCountRelevant(t *testing.T) {
	judgments := []Judgment{
		{Path: "a.py", Grade: 2},
		{Path: "b.py", Grade: 0},
		{Path: "c.py", Grade: 1},
	}
	got := countRelevant(judgments)
	if got != 2 {
		t.Errorf("countRelevant() = %d, want 2", got)
	}
}

func TestHitRelevance(t *testing.T) {
	gradeMap := map[string]int{
		"code-python/session_manager.py": 2,
		"code-go/config.go":              1,
	}

	tests := []struct {
		hitPath string
		want    int
	}{
		{"/workspace/code-python/session_manager.py", 2},
		{"/workspace/code-go/config.go", 1},
		{"/workspace/unrelated.txt", 0},
	}

	for _, tt := range tests {
		got := hitRelevance(tt.hitPath, gradeMap)
		if got != tt.want {
			t.Errorf("hitRelevance(%q) = %d, want %d", tt.hitPath, got, tt.want)
		}
	}
}
