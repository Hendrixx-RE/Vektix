package resolve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Hendrixx-RE/Vektix/internal/index"
	"github.com/Hendrixx-RE/Vektix/internal/ollama"
	"github.com/Hendrixx-RE/Vektix/internal/store"
)

func makeVector(v1, v2 float32) []float32 {
	v := make([]float32, 768)
	v[0] = v1
	v[1] = v2
	return v
}

func TestResolve(t *testing.T) {
	// 1. Mock Ollama
	embedMap := map[string][]float32{
		"search_query: ERR_CONN_REFUSED":         makeVector(0.0, 1.0),
		"search_document: func connect() { return ERR_CONN_REFUSED }": makeVector(0.0, -1.0), // orthogonal
		
		"search_query: docker notes":             makeVector(1.0, 1.0),
		"search_document: Here is some container stuff for deployment.": makeVector(1.0, 0.9), // closely related
		"search_document: Docker is a great tool for containers.": makeVector(1.0, 0.2), // less related
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Texts []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		
		var res [][]float32
		for _, text := range req.Texts {
			vec, ok := embedMap[text]
			if !ok {
				vec = makeVector(0.0, 0.0) // generic
			}
			res = append(res, vec)
		}

		json.NewEncoder(w).Encode(map[string]any{"embeddings": res})
	}))
	defer ts.Close()

	opts := ollama.Options{
		Host:              ts.URL,
		EmbedTimeout:      5 * time.Second,
		IntentTimeout:     5 * time.Second,
		StreamIdleTimeout: 5 * time.Second,
	}
	client := ollama.NewClient(opts)

	// 2. Setup DB and Chunks
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "db")
	db, err := store.NewPersistentDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	chunks := []store.Chunk{
		{ID: "doc1", Path: "/projects/app/main.go", Content: "func connect() { return ERR_CONN_REFUSED }", Embedding: embedMap["search_document: func connect() { return ERR_CONN_REFUSED }"]},
		{ID: "doc2", Path: "/projects/app/deploy.md", Content: "Here is some container stuff for deployment.", Embedding: embedMap["search_document: Here is some container stuff for deployment."]},
		{ID: "doc3", Path: "/notes/docker.md", Content: "Docker is a great tool for containers.", Embedding: embedMap["search_document: Docker is a great tool for containers."]},
	}

	ctx := context.Background()
	if err := db.AddDocuments(ctx, chunks); err != nil {
		t.Fatal(err)
	}

	manifest := &index.Manifest{
		DirCounts: map[string]int{
			"": 3,
			"/projects/app": 2,
			"/notes": 1,
		},
	}

	cache := ollama.NewEmbeddingCache(100)
	vectorArm := NewVectorArm(db, client, cache, manifest, "nomic-embed-text", "5m", 0.01)
	bm25Arm := NewBM25Index(chunks)
	pathArm := NewPathIndex(chunks)

	runSearch := func(query string, scope string) ([]ScoredChunk, ResultList, ResultList) {
		vRes, _ := vectorArm.Search(ctx, query, scope, 5)
		bRes := bm25Arm.Search(query, scope)
		pRes := pathArm.Search(query, scope)
		fRes := Fuse([]ResultList{vRes, bRes, pRes}, 60, 1, 5)
		return fRes, vRes, bRes
	}

	// EXACT TOKEN: ERR_CONN_REFUSED
	fRes, _, bRes := runSearch("ERR_CONN_REFUSED", "")
	if len(bRes) == 0 || bRes[0].ID != "doc1" {
		t.Errorf("BM25 should win exact token")
	}
	if len(fRes) == 0 || fRes[0].ID != "doc1" {
		t.Errorf("RRF should rank doc1 first for ERR_CONN_REFUSED")
	}

	// PARAPHRASE: "container stuff" via "docker notes"
	fRes, vRes, bRes := runSearch("docker notes", "")
	if len(vRes) == 0 || vRes[0].ID != "doc2" {
		if len(vRes) > 0 {
			t.Errorf("Vector should win paraphrase case, got doc ID: %s", vRes[0].ID)
		} else {
			t.Errorf("Vector should win paraphrase case, but got empty result")
		}
	}
	if len(bRes) == 0 || bRes[0].ID != "doc3" {
		if len(bRes) > 0 {
			t.Errorf("BM25 should match doc3 for 'docker notes', got doc ID: %s", bRes[0].ID)
		} else {
			t.Errorf("BM25 should match doc3 for 'docker notes', but got empty result")
		}
	}
	
	// SCOPED VS GLOBAL
	fResScoped, _, _ := runSearch("docker notes", "/projects/app")
	foundDoc3 := false
	for _, r := range fResScoped {
		if r.ID == "doc3" {
			foundDoc3 = true
		}
	}
	if foundDoc3 {
		t.Errorf("Scoped search should NOT return doc3")
	}
	if len(fResScoped) == 0 || fResScoped[0].ID != "doc2" {
		t.Errorf("Scoped search should return doc2")
	}
}
