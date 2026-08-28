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
