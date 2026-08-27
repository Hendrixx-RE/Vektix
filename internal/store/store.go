package store

import (
	"context"
	"fmt"
	"runtime"

	"github.com/philippgille/chromem-go"
)

const defaultCollection = "vektix"

// Store wraps chromem-go for the Vektix application.
type Store struct {
	db  *chromem.DB
	col *chromem.Collection
}

// NewPersistentDB opens a persistent vector store at the given path.
func NewPersistentDB(path string) (*Store, error) {
	db, err := chromem.NewPersistentDB(path, false)
	if err != nil {
		return nil, fmt.Errorf("failed to open db: %w", err)
	}

	col, err := db.GetOrCreateCollection(defaultCollection, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create collection: %w", err)
	}

	return &Store{
		db:  db,
		col: col,
	}, nil
}

// AddDocuments adds multiple chunks to the collection concurrently.
func (s *Store) AddDocuments(ctx context.Context, chunks []Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	docs := make([]chromem.Document, len(chunks))
	for i, c := range chunks {
		docs[i] = chromem.Document{
			ID:        c.ID,
			Metadata:  c.EncodeMetadata(),
			Embedding: c.Embedding,
			Content:   c.Content,
		}
	}

	concurrency := runtime.NumCPU()
	if concurrency > len(docs) {
		concurrency = len(docs)
	}

	return s.col.AddDocuments(ctx, docs, concurrency)
}

// QueryEmbedding performs an exhaustive search in the collection using the provided embedding.
func (s *Store) QueryEmbedding(ctx context.Context, embedding []float32, nResults int, where map[string]string, whereDocument map[string]string) ([]Chunk, error) {
	if nResults <= 0 {
		return nil, nil
	}

	count := s.col.Count()
	if count == 0 {
		return nil, nil
	}
	if nResults > count {
		nResults = count
	}

	res, err := s.col.QueryEmbedding(ctx, embedding, nResults, where, whereDocument)
	if err != nil {
		return nil, err
	}

	var chunks []Chunk
	for _, r := range res {
		chunk, err := DecodeMetadata(r.Metadata)
		if err != nil {
			return nil, fmt.Errorf("decode metadata for %s: %w", r.ID, err)
		}
		chunk.ID = r.ID
		chunk.Content = r.Content
		chunk.Embedding = r.Embedding
		chunks = append(chunks, chunk)
	}

	return chunks, nil
}

// Delete removes chunks matching the specified where conditions or IDs.
func (s *Store) Delete(ctx context.Context, where map[string]string, whereDocument map[string]string, ids ...string) error {
	return s.col.Delete(ctx, where, whereDocument, ids...)
}

// Count returns the total number of chunks in the store.
func (s *Store) Count() int {
	return s.col.Count()
}
