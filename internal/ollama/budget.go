package ollama

import (
	"log"
)

// Chunk represents a piece of text to be sent in context, along with its rank.
// Lower rank numbers (e.g., 1) are better.
type Chunk struct {
	Text string
	Rank int
}

// EstimateTokens provides a rough estimate of tokens using rune length divided by 4,
// as specified in the Vektix plan.
func EstimateTokens(s string) int {
	return len([]rune(s)) / 4
}

// EnforceBudget drops the lowest-ranked chunks to fit within maxCtx tokens.
// promptTokens includes the estimated tokens of the system prompt, instructions, etc.
// chunks should be sorted by Rank in ascending order (best chunks first).
// Returns the subset of chunks that fit within the budget.
func EnforceBudget(promptTokens int, chunks []Chunk, maxCtx int) []Chunk {
	total := promptTokens
	chunkTokens := make([]int, len(chunks))
	
	for i, c := range chunks {
		ct := EstimateTokens(c.Text)
		chunkTokens[i] = ct
		total += ct
	}

	if total <= maxCtx {
		return chunks
	}

	dropped := 0
	// Drop from the end (lowest ranked chunks) until we fit.
	for i := len(chunks) - 1; i >= 0; i-- {
		total -= chunkTokens[i]
		dropped++
		if total <= maxCtx {
			log.Printf("budget: dropped %d chunks to fit num_ctx of %d", dropped, maxCtx)
			return chunks[:i]
		}
	}

	log.Printf("budget: dropped all %d chunks to fit num_ctx of %d", dropped, maxCtx)
	return nil
}
