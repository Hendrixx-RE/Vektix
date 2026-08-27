package resolve

import (
	"context"
	"strings"

	"github.com/Hendrixx-RE/Vektix/internal/index"
	"github.com/Hendrixx-RE/Vektix/internal/ollama"
	"github.com/Hendrixx-RE/Vektix/internal/store"
)

// VectorArm wraps chromem-go and Ollama embedding for hybrid search.
type VectorArm struct {
	store      *store.Store
	client     *ollama.Client
	cache      *ollama.EmbeddingCache
	manifest   *index.Manifest
	model      string
	keepAlive  string
	oversample float64
}

// NewVectorArm creates a new VectorArm.
func NewVectorArm(db *store.Store, client *ollama.Client, cache *ollama.EmbeddingCache, manifest *index.Manifest, model, keepAlive string, oversampleFloor float64) *VectorArm {
	return &VectorArm{
		store:      db,
		client:     client,
		cache:      cache,
		manifest:   manifest,
		model:      model,
		keepAlive:  keepAlive,
		oversample: oversampleFloor,
	}
}

// Search performs adaptive oversampled vector search, filtering by scope in Go.
func (v *VectorArm) Search(ctx context.Context, query string, scope string, k int) (ResultList, error) {
	if query == "" {
		return nil, nil
	}

	emb, found := v.cache.Get(query)
	if !found {
		req := ollama.EmbedRequest{
			Model:     v.model,
			Texts:     []string{query},
			IsQuery:   true,
			KeepAlive: v.keepAlive,
		}
		resp, err := v.client.Embed(ctx, req)
		if err != nil {
			return nil, err
		}
		if len(resp.Embeddings) > 0 {
			emb = resp.Embeddings[0]
			v.cache.Put(query, emb)
		}
	}

	if len(emb) == 0 {
		return nil, nil
	}

	collectionSize := v.store.Count()
	if collectionSize == 0 {
		return nil, nil
	}

	nResults := k
	if scope != "" {
		scopeFraction := v.manifest.ScopeFraction(scope)
		if scopeFraction < v.oversample {
			scopeFraction = v.oversample
		}
		
		nResults = int(float64(k) / scopeFraction)
		if nResults < k {
			nResults = k
		}
		if nResults > collectionSize {
			nResults = collectionSize
		}
	}

	// First pass
	results, err := v.store.QueryEmbedding(ctx, emb, nResults, nil, nil)
	if err != nil {
		return nil, err
	}

	filtered := filterScope(results, scope)
	if len(filtered) < k && nResults < collectionSize {
		// Retry exhaustively
		results, err = v.store.QueryEmbedding(ctx, emb, collectionSize, nil, nil)
		if err != nil {
			return nil, err
		}
		filtered = filterScope(results, scope)
	}

	if len(filtered) > k {
		filtered = filtered[:k]
	}

	return filtered, nil
}

func filterScope(results []store.Chunk, scope string) []store.Chunk {
	if scope == "" {
		return results
	}
	var filtered []store.Chunk
	for _, r := range results {
		if strings.HasPrefix(r.Path, scope) {
			filtered = append(filtered, r)
		}
	}
	return filtered
}
