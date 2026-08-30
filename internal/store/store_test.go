package store

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
)

func TestStore_AddQueryDelete(t *testing.T) {
	tempDir := filepath.Join(t.TempDir(), "store_test")

	s, err := NewPersistentDB(tempDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	chunks := []Chunk{
		{
			ID:        "doc-1",
			Path:      "a/b/c.txt",
			Content:   "hello world",
			Embedding: []float32{1.0, 0.0},
			Locator: Locator{
				Kind:  LocatorLineRange,
				Start: 1,
				End:   10,
			},
		},
		{
			ID:        "doc-2",
			Path:      "a/b/d.txt",
			Content:   "test document",
			Embedding: []float32{0.0, 1.0},
			Locator: Locator{
				Kind:  LocatorLineRange,
				Start: 1,
				End:   5,
			},
		},
	}

	ctx := context.Background()

	// Add
	err = s.AddDocuments(ctx, chunks)
	if err != nil {
		t.Fatalf("failed to add documents: %v", err)
	}

	// Query exactly for doc-1's path
	where := map[string]string{
		"path": "a/b/c.txt",
	}

	res, err := s.QueryEmbedding(ctx, []float32{1.0, 0.0}, 5, where, nil)
	if err != nil {
		t.Fatalf("failed to query embedding: %v", err)
	}

	if len(res) != 1 {
		t.Fatalf("expected 1 result, got %d", len(res))
	}

	if res[0].ID != "doc-1" {
		t.Errorf("expected doc-1, got %s", res[0].ID)
	}
	if res[0].Path != "a/b/c.txt" {
		t.Errorf("expected a/b/c.txt, got %s", res[0].Path)
	}
	if !reflect.DeepEqual(res[0].Locator, chunks[0].Locator) {
		t.Errorf("expected locator %+v, got %+v", chunks[0].Locator, res[0].Locator)
	}

	// Delete doc-1 by path
	err = s.Delete(ctx, where, nil)
	if err != nil {
		t.Fatalf("failed to delete document: %v", err)
	}

	// Query again, should not find doc-1
	res2, err := s.QueryEmbedding(ctx, []float32{1.0, 0.0}, 5, where, nil)
	if err != nil {
		t.Fatalf("failed to query embedding: %v", err)
	}

	if len(res2) != 0 {
		t.Fatalf("expected 0 results after delete, got %d", len(res2))
	}
}

func TestStore_EmptyAdd(t *testing.T) {
	tempDir := filepath.Join(t.TempDir(), "store_empty_test")
	s, err := NewPersistentDB(tempDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	err = s.AddDocuments(context.Background(), nil)
	if err != nil {
		t.Fatalf("AddDocuments with empty slice failed: %v", err)
	}
}

func TestChunk_TransientMetadata(t *testing.T) {
	c := Chunk{
		ID:        "doc-transient",
		Path:      "/tmp/project/foo.go",
		Content:   "func foo() {}",
		Transient: true,
		Locator: Locator{
			Kind:   LocatorSymbol,
			Start:  1,
			End:    3,
			Symbol: "func foo()",
		},
	}

	meta := c.EncodeMetadata()
	if meta["transient"] != "true" {
		t.Errorf("expected transient='true', got %q", meta["transient"])
	}

	decoded, err := DecodeMetadata(meta)
	if err != nil {
		t.Fatalf("DecodeMetadata failed: %v", err)
	}
	if !decoded.Transient {
		t.Errorf("expected decoded.Transient == true")
	}
	if decoded.Locator.Symbol != "func foo()" {
		t.Errorf("expected symbol 'func foo()', got %q", decoded.Locator.Symbol)
	}

	cNonTransient := Chunk{
		ID:        "doc-perm",
		Path:      "/tmp/project/bar.go",
		Transient: false,
	}
	metaNonTransient := cNonTransient.EncodeMetadata()
	if _, ok := metaNonTransient["transient"]; ok {
		t.Errorf("expected no 'transient' key for non-transient chunk, got %q", metaNonTransient["transient"])
	}
	decodedNonTransient, err := DecodeMetadata(metaNonTransient)
	if err != nil {
		t.Fatalf("DecodeMetadata failed: %v", err)
	}
	if decodedNonTransient.Transient {
		t.Errorf("expected decodedNonTransient.Transient == false")
	}
}

func TestStore_GetByIDs(t *testing.T) {
	dir := t.TempDir()
	st, err := NewPersistentDB(dir)
	if err != nil {
		t.Fatal(err)
	}

	chunks := []Chunk{
		{ID: "c1", Content: "content 1", Path: "/a/1.go", Embedding: []float32{1.0, 0.0}},
		{ID: "c2", Content: "content 2", Path: "/a/2.go", Embedding: []float32{0.0, 1.0}},
		{ID: "c3", Content: "content 3", Path: "/a/3.go", Embedding: []float32{1.0, 1.0}},
		{ID: "c4", Content: "content 4", Path: "/a/4.go", Embedding: []float32{0.5, 0.5}},
	}

	if err := st.AddDocuments(context.Background(), chunks); err != nil {
		t.Fatalf("AddDocuments failed: %v", err)
	}

	// 1. Fetch subset with existing IDs and one missing ID
	res, err := st.GetByIDs(context.Background(), []string{"c3", "c1", "missing", "c4"})
	if err != nil {
		t.Fatalf("GetByIDs failed: %v", err)
	}

	if len(res) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(res))
	}
	if res[0].ID != "c3" || res[1].ID != "c1" || res[2].ID != "c4" {
		t.Errorf("unexpected order of chunks: %+v", res)
	}

	// 2. Fetch empty slice
	emptyRes, err := st.GetByIDs(context.Background(), nil)
	if err != nil || len(emptyRes) != 0 {
		t.Errorf("expected empty result, got %v / %v", emptyRes, err)
	}
}
