package ollama

import (
	"reflect"
	"testing"
)

func TestEmbeddingCache(t *testing.T) {
	cache := NewEmbeddingCache(2)

	// Test Miss
	if _, ok := cache.Get("q1"); ok {
		t.Errorf("expected cache miss for q1")
	}

	// Test Put and Get
	e1 := []float32{0.1, 0.2}
	cache.Put("q1", e1)
	if v, ok := cache.Get("q1"); !ok || !reflect.DeepEqual(v, e1) {
		t.Errorf("expected hit for q1 with %v, got %v (hit: %v)", e1, v, ok)
	}

	// Test Eviction
	e2 := []float32{0.3, 0.4}
	cache.Put("q2", e2)
	e3 := []float32{0.5, 0.6}
	cache.Put("q3", e3)

	// q1 should be evicted because capacity is 2 and we inserted q2, q3
	if _, ok := cache.Get("q1"); ok {
		t.Errorf("expected cache miss for q1, should have been evicted")
	}
	if _, ok := cache.Get("q2"); !ok {
		t.Errorf("expected cache hit for q2")
	}
	if _, ok := cache.Get("q3"); !ok {
		t.Errorf("expected cache hit for q3")
	}

	// Test LRU update on Get
	// Currently q3 is MRU (since last inserted), but Get("q2") makes q2 MRU
	_, _ = cache.Get("q2")
	
	e4 := []float32{0.7, 0.8}
	cache.Put("q4", e4) // This should evict q3, not q2

	if _, ok := cache.Get("q3"); ok {
		t.Errorf("expected cache miss for q3, should have been evicted")
	}
	if _, ok := cache.Get("q2"); !ok {
		t.Errorf("expected cache hit for q2")
	}
	if _, ok := cache.Get("q4"); !ok {
		t.Errorf("expected cache hit for q4")
	}

	// Test Put update existing
	e4_new := []float32{0.9, 1.0}
	cache.Put("q4", e4_new)
	if v, ok := cache.Get("q4"); !ok || !reflect.DeepEqual(v, e4_new) {
		t.Errorf("expected q4 to be updated to %v", e4_new)
	}
}
