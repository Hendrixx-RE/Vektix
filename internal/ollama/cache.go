package ollama

import (
	"container/list"
	"sync"
)

type cacheEntry struct {
	query     string
	embedding []float32
}

// EmbeddingCache is a concurrency-safe LRU cache for query embeddings.
type EmbeddingCache struct {
	mu       sync.Mutex
	capacity int
	lru      *list.List
	cache    map[string]*list.Element
}

// NewEmbeddingCache creates a new LRU cache with the specified capacity.
func NewEmbeddingCache(capacity int) *EmbeddingCache {
	return &EmbeddingCache{
		capacity: capacity,
		lru:      list.New(),
		cache:    make(map[string]*list.Element),
	}
}

// Get returns the embedding for a query if it exists in the cache.
func (c *EmbeddingCache) Get(query string) ([]float32, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.cache[query]; ok {
		c.lru.MoveToFront(elem)
		return elem.Value.(*cacheEntry).embedding, true
	}
	return nil, false
}

// Put adds an embedding to the cache for the given query.
func (c *EmbeddingCache) Put(query string, embedding []float32) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.cache[query]; ok {
		c.lru.MoveToFront(elem)
		elem.Value.(*cacheEntry).embedding = embedding
		return
	}

	if c.lru.Len() >= c.capacity {
		back := c.lru.Back()
		if back != nil {
			c.lru.Remove(back)
			delete(c.cache, back.Value.(*cacheEntry).query)
		}
	}

	elem := c.lru.PushFront(&cacheEntry{
		query:     query,
		embedding: embedding,
	})
	c.cache[query] = elem
}
