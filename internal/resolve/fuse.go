package resolve

import (
	"sort"

	"github.com/Hendrixx-RE/Vektix/internal/store"
)

// ScoredChunk wraps a chunk with its fusion score.
type ScoredChunk struct {
	store.Chunk
	Score float64
	Arms  int
}

// ResultList is a ranked list of chunks from a single search arm.
type ResultList []store.Chunk

// Fuse combines results from multiple arms using Reciprocal Rank Fusion.
// It applies a min_arms threshold and returns up to max_results.
func Fuse(arms []ResultList, rrfK int, minArms int, maxResults int) []ScoredChunk {
	scores := make(map[string]*ScoredChunk)

	for _, arm := range arms {
		for rank, chunk := range arm {
			// Rank is 0-indexed, but usually RRF formula rank is 1-indexed.
			// The plan.md says: score(doc) = Σ_arms  1 / (60 + rank_arm(doc))
			// We will treat rank_arm as 1-indexed.
			r := rank + 1
			if _, exists := scores[chunk.ID]; !exists {
				scores[chunk.ID] = &ScoredChunk{
					Chunk: chunk,
					Score: 0,
					Arms:  0,
				}
			}
			scores[chunk.ID].Score += 1.0 / float64(rrfK+r)
			scores[chunk.ID].Arms++
		}
	}

	var results []ScoredChunk
	for _, sc := range scores {
		if sc.Arms >= minArms {
			results = append(results, *sc)
		}
	}

	// Sort descending by score
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > maxResults {
		results = results[:maxResults]
	}

	return results
}
