package store

import (
	"context"
	"fmt"
)

// GetByID returns a single chunk by its ID, decoding the locator metadata.
// The CLI uses it to rehydrate the corpus listed in the manifest for the
// model-free arms (path + BM25), which need chunk text but no embedding.
func (s *Store) GetByID(ctx context.Context, id string) (Chunk, error) {
	doc, err := s.col.GetByID(ctx, id)
	if err != nil {
		return Chunk{}, err
	}

	chunk, err := DecodeMetadata(doc.Metadata)
	if err != nil {
		return Chunk{}, fmt.Errorf("decode metadata for %s: %w", id, err)
	}
	chunk.ID = doc.ID
	chunk.Content = doc.Content
	chunk.Embedding = doc.Embedding

	return chunk, nil
}

// GetByIDs returns chunks for all given IDs in a concurrent batch, decoding metadata.
// Returned slice preserves the order of IDs and omits missing/unreadable chunks.
func (s *Store) GetByIDs(ctx context.Context, ids []string) ([]Chunk, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	numWorkers := 8
	if numWorkers > len(ids) {
		numWorkers = len(ids)
	}

	type result struct {
		idx   int
		chunk Chunk
		err   error
	}

	jobs := make(chan int, len(ids))
	results := make(chan result, len(ids))

	for w := 0; w < numWorkers; w++ {
		go func() {
			for idx := range jobs {
				if ctx.Err() != nil {
					results <- result{idx: idx, err: ctx.Err()}
					continue
				}
				doc, err := s.col.GetByID(ctx, ids[idx])
				if err != nil {
					results <- result{idx: idx, err: err}
					continue
				}
				chunk, err := DecodeMetadata(doc.Metadata)
				if err != nil {
					results <- result{idx: idx, err: fmt.Errorf("decode metadata for %s: %w", ids[idx], err)}
					continue
				}
				chunk.ID = doc.ID
				chunk.Content = doc.Content
				chunk.Embedding = doc.Embedding
				results <- result{idx: idx, chunk: chunk}
			}
		}()
	}

	for i := range ids {
		jobs <- i
	}
	close(jobs)

	chunks := make([]Chunk, len(ids))
	found := make([]bool, len(ids))

	for i := 0; i < len(ids); i++ {
		r := <-results
		if r.err == nil {
			chunks[r.idx] = r.chunk
			found[r.idx] = true
		}
	}

	out := make([]Chunk, 0, len(ids))
	for i, ok := range found {
		if ok {
			out = append(out, chunks[i])
		}
	}
	return out, nil
}
