package ollama

import (
	"testing"
)

func TestEstimateTokens(t *testing.T) {
	cases := []struct {
		input    string
		expected int
	}{
		{"", 0},
		{"abcde", 1},
		{"Hello, World! This is a test.", 7}, // len=29, /4 = 7
		{"🚀🌟🔥", 0}, // len=3, /4 = 0
	}

	for _, c := range cases {
		if got := EstimateTokens(c.input); got != c.expected {
			t.Errorf("EstimateTokens(%q) = %d; want %d", c.input, got, c.expected)
		}
	}
}

func TestEnforceBudget(t *testing.T) {
	chunks := []Chunk{
		{Text: "chunk 1 needs 4 runes.", Rank: 1}, // 22 runes -> 5 tokens
		{Text: "chunk 2 needs 4 runes.", Rank: 2}, // 22 runes -> 5 tokens
		{Text: "chunk 3 needs 4 runes.", Rank: 3}, // 22 runes -> 5 tokens
	}

	// Total chunk tokens: 5 + 5 + 5 = 15.
	// promptTokens = 5
	// Total requested = 20.

	// Case 1: Budget big enough
	res := EnforceBudget(5, chunks, 25)
	if len(res) != 3 {
		t.Errorf("expected 3 chunks, got %d", len(res))
	}

	// Case 2: Exact fit
	res = EnforceBudget(5, chunks, 20)
	if len(res) != 3 {
		t.Errorf("expected 3 chunks, got %d", len(res))
	}

	// Case 3: Need to drop 1
	res = EnforceBudget(5, chunks, 19)
	if len(res) != 2 {
		t.Errorf("expected 2 chunks, got %d", len(res))
	}
	if len(res) > 0 && res[len(res)-1].Rank != 2 {
		t.Errorf("dropped wrong chunk")
	}

	// Case 4: Drop all
	res = EnforceBudget(5, chunks, 4)
	if len(res) != 0 {
		t.Errorf("expected 0 chunks, got %d", len(res))
	}
}
