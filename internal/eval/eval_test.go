package eval

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hendrixx-RE/Vektix/internal/config"
	"github.com/Hendrixx-RE/Vektix/internal/ollama"
	"github.com/Hendrixx-RE/Vektix/internal/router"
)

func TestDetectDatasetType(t *testing.T) {
	intentFile := filepath.Join(t.TempDir(), "intent.jsonl")
	_ = os.WriteFile(intentFile, []byte(`{"input": "open main.go", "expected": {"action": "open", "path": "main.go"}, "tier": 1}`), 0644)

	locateFile := filepath.Join(t.TempDir(), "locate.jsonl")
	_ = os.WriteFile(locateFile, []byte(`{"query": "jwt auth", "expect_path": "pkg/auth/jwt.go", "scope": "global"}`), 0644)

	invalidFile := filepath.Join(t.TempDir(), "invalid.jsonl")
	_ = os.WriteFile(invalidFile, []byte(`{"foo": "bar"}`), 0644)

	t.Run("Intent", func(t *testing.T) {
		dt, err := DetectDatasetType(intentFile)
		if err != nil || dt != DatasetTypeIntent {
			t.Fatalf("expected DatasetTypeIntent, got %v (err: %v)", dt, err)
		}
	})

	t.Run("Locate", func(t *testing.T) {
		dt, err := DetectDatasetType(locateFile)
		if err != nil || dt != DatasetTypeLocate {
			t.Fatalf("expected DatasetTypeLocate, got %v (err: %v)", dt, err)
		}
	})

	t.Run("Unknown", func(t *testing.T) {
		dt, err := DetectDatasetType(invalidFile)
		if err != nil || dt != DatasetTypeUnknown {
			t.Fatalf("expected DatasetTypeUnknown, got %v (err: %v)", dt, err)
		}
	})
}

func TestCheckIntentParams(t *testing.T) {
	tests := []struct {
		name     string
		expected *router.Intent
		actual   *router.Intent
		want     bool
	}{
		{
			name:     "both nil",
			expected: nil,
			actual:   nil,
			want:     true,
		},
		{
			name:     "exact path match",
			expected: &router.Intent{Action: "open", Path: "main.go"},
			actual:   &router.Intent{Action: "open", Path: "main.go"},
			want:     true,
		},
		{
			name:     "path normalized match",
			expected: &router.Intent{Action: "open", Path: "pkg/auth/jwt.go"},
			actual:   &router.Intent{Action: "open", Path: "pkg/auth/jwt.go"},
			want:     true,
		},
		{
			name:     "query substring match",
			expected: &router.Intent{Action: "excerpt", Query: "docker"},
			actual:   &router.Intent{Action: "excerpt", Query: "what I wrote about docker"},
			want:     true,
		},
		{
			name:     "lines match",
			expected: &router.Intent{Action: "read", Path: "main.go", Lines: "10-20"},
			actual:   &router.Intent{Action: "read", Path: "main.go", Lines: "10-20"},
			want:     true,
		},
		{
			name:     "path mismatch",
			expected: &router.Intent{Action: "open", Path: "main.go"},
			actual:   &router.Intent{Action: "open", Path: "server.go"},
			want:     false,
		},
		{
			name:     "lines mismatch",
			expected: &router.Intent{Action: "read", Path: "main.go", Lines: "10-20"},
			actual:   &router.Intent{Action: "read", Path: "main.go", Lines: "30-40"},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CheckIntentParams(tt.expected, tt.actual)
			if got != tt.want {
				t.Errorf("CheckIntentParams() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCheckPathMatch(t *testing.T) {
	tests := []struct {
		actual string
		expect string
		want   bool
	}{
		{"/home/user/project/pkg/auth/jwt.go", "pkg/auth/jwt.go", true},
		{"pkg/auth/jwt.go", "pkg/auth/jwt.go", true},
		{"/var/data/server.go", "server.go", true},
		{"pkg/auth/jwt.go", "pkg/db/pool.go", false},
		{"", "pkg/auth/jwt.go", false},
	}

	for _, tt := range tests {
		got := CheckPathMatch(tt.actual, tt.expect)
		if got != tt.want {
			t.Errorf("CheckPathMatch(%q, %q) = %v, want %v", tt.actual, tt.expect, got, tt.want)
		}
	}
}

func TestCheckExcerptCorrectness(t *testing.T) {
	content := "func ValidateToken(token string) (*Claims, error) {\n    return nil, nil\n}"
	if !CheckExcerptCorrectness(content, "ValidateToken") {
		t.Errorf("expected excerpt correctness to be true")
	}
	if CheckExcerptCorrectness(content, "NonExistentFunction") {
		t.Errorf("expected excerpt correctness to be false")
	}
	if !CheckExcerptCorrectness(content, "") {
		t.Errorf("expected empty string to be true")
	}
}

func TestComputePercentile(t *testing.T) {
	durations := []time.Duration{
		10 * time.Millisecond,
		20 * time.Millisecond,
		30 * time.Millisecond,
		40 * time.Millisecond,
		50 * time.Millisecond,
		60 * time.Millisecond,
		70 * time.Millisecond,
		80 * time.Millisecond,
		90 * time.Millisecond,
		100 * time.Millisecond,
	}

	p50 := ComputePercentile(durations, 0.50)
	if p50 != 50*time.Millisecond {
		t.Errorf("ComputePercentile(0.50) = %v, want 50ms", p50)
	}

	p95 := ComputePercentile(durations, 0.95)
	if p95 != 100*time.Millisecond {
		t.Errorf("ComputePercentile(0.95) = %v, want 100ms", p95)
	}

	if p0 := ComputePercentile(nil, 0.50); p0 != 0 {
		t.Errorf("expected 0 for empty slice, got %v", p0)
	}
}

func TestComputeMetricsOutput(t *testing.T) {
	locMetrics := LocateMetrics{
		TotalCases:      10,
		LocateRecallAt1: 80.0,
		LocateRecallAt3: 90.0,
		ScopedRecallAt1: 85.0,
		ScopedRecallAt3: 95.0,
		ScopedDeltaAt1:  5.0,
		ScopedDeltaAt3:  5.0,
		BM25Recall:      70.0,
		VectorRecall:    75.0,
		RRFRecall:       90.0,
		P50Latency:      41 * time.Millisecond,
		P95Latency:      118 * time.Millisecond,
		HasExcerptCases: true,
		ExcerptAccuracy: 100.0,
	}

	str := locMetrics.String()
	if !strings.Contains(str, "locate recall@1   80.0%") || !strings.Contains(str, "(+5.0 / +5.0)") {
		t.Errorf("unexpected LocateMetrics.String() output: %s", str)
	}

	intentMetrics := IntentMetrics{
		TotalCases:     10,
		ActionAccuracy: 95.0,
		ParamAccuracy:  90.0,
		Tier1Rate:      50.0,
		P50Latency:     2 * time.Millisecond,
		P95Latency:     50 * time.Millisecond,
	}

	intentStr := intentMetrics.String()
	if !strings.Contains(intentStr, "intent action accuracy       95.0%") {
		t.Errorf("unexpected IntentMetrics.String() output: %s", intentStr)
	}
}

func TestRunIntentMocked(t *testing.T) {
	// Mock Ollama chat endpoint for Tier 2 fallback
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/chat" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"message": {"role": "assistant", "content": "{\"action\":\"excerpt\",\"query\":\"docker\"}"}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	datasetContent := `{"input": "open main.go", "expected": {"action": "open", "path": "main.go"}, "tier": 1}
{"input": "what did I write about docker", "expected": {"action": "excerpt", "query": "docker"}, "tier": 2}`

	datasetPath := filepath.Join(t.TempDir(), "test_intent.jsonl")
	if err := os.WriteFile(datasetPath, []byte(datasetContent), 0644); err != nil {
		t.Fatalf("failed writing dataset: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Ollama.Host = ts.URL

	client := ollama.NewClient(ollama.Options{
		Host:          ts.URL,
		IntentTimeout: 2 * time.Second,
	})

	metrics, results, err := RunIntent(context.Background(), datasetPath, RunnerOptions{
		Config: &cfg,
		Client: client,
	})
	if err != nil {
		t.Fatalf("RunIntent failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if metrics.ActionAccuracy != 100.0 {
		t.Errorf("expected 100%% action accuracy, got %.1f%%", metrics.ActionAccuracy)
	}
}

func TestRunLocateMocked(t *testing.T) {
	// Mock Ollama embed endpoint
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/embed" {
			var req struct {
				Input []string `json:"input"`
			}
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &req)

			embeddings := make([][]float32, len(req.Input))
			for i := range req.Input {
				// Deterministic mock embedding vector
				embeddings[i] = []float32{0.1, 0.2, 0.3, 0.4}
			}

			resp := struct {
				Embeddings [][]float32 `json:"embeddings"`
			}{Embeddings: embeddings}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	corpusDir := "../../testdata/corpus"
	if _, err := os.Stat(corpusDir); err != nil {
		corpusDir = "testdata/corpus"
	}

	cfg := config.DefaultConfig()
	cfg.Ollama.Host = ts.URL

	client := ollama.NewClient(ollama.Options{
		Host:         ts.URL,
		EmbedTimeout: 5 * time.Second,
	})

	dataDir := filepath.Join(t.TempDir(), "eval_store")

	runnerOpts := RunnerOptions{
		Config:    &cfg,
		Client:    client,
		CorpusDir: corpusDir,
		DataDir:   dataDir,
		Limit:     3,
	}

	corpusIndex, err := LoadOrIndexCorpus(context.Background(), runnerOpts)
	if err != nil {
		t.Fatalf("LoadOrIndexCorpus failed: %v", err)
	}

	if len(corpusIndex.Chunks) == 0 {
		t.Fatalf("expected chunks in corpus, got 0")
	}

	datasetContent := `{"query": "jwt.go", "expect_path": "pkg/auth/jwt.go", "scope": "global", "expected_text": "Claims"}
{"query": "MaxOpenConns", "expect_path": "pkg/db/pool.go", "scope": "pkg/db", "expected_text": "MaxOpenConns"}`

	datasetPath := filepath.Join(t.TempDir(), "test_locate.jsonl")
	if err := os.WriteFile(datasetPath, []byte(datasetContent), 0644); err != nil {
		t.Fatalf("failed writing locate dataset: %v", err)
	}

	metrics, results, err := RunLocate(context.Background(), corpusIndex, datasetPath, runnerOpts)
	if err != nil {
		t.Fatalf("RunLocate failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if metrics.LocateRecallAt3 < 50.0 {
		t.Errorf("expected locate recall@3 >= 50%%, got %.1f%%", metrics.LocateRecallAt3)
	}
	if metrics.ExcerptAccuracy < 50.0 {
		t.Errorf("expected excerpt accuracy >= 50%%, got %.1f%%", metrics.ExcerptAccuracy)
	}
}

func TestDatasetIntegrationConsistency(t *testing.T) {
	// Verify testdata/locate_eval.jsonl parses cleanly
	f, err := os.Open("../../testdata/locate_eval.jsonl")
	if err != nil {
		f, err = os.Open("testdata/locate_eval.jsonl")
		if err != nil {
			t.Fatalf("failed opening testdata/locate_eval.jsonl: %v", err)
		}
	}
	defer f.Close()

	cases, err := LoadLocateCases(f)
	if err != nil {
		t.Fatalf("LoadLocateCases failed: %v", err)
	}
	if len(cases) < 20 {
		t.Errorf("expected >= 20 locate cases in locate_eval.jsonl, got %d", len(cases))
	}
}

func TestLoadOrIndexCorpusUnreachableOllama(t *testing.T) {
	corpusDir := "../../testdata/corpus"
	if _, err := os.Stat(corpusDir); err != nil {
		corpusDir = "testdata/corpus"
	}

	cfg := config.DefaultConfig()
	// Point to unreachable port
	cfg.Ollama.Host = "http://127.0.0.1:54321"

	client := ollama.NewClient(ollama.Options{
		Host:         cfg.Ollama.Host,
		EmbedTimeout: 100 * time.Millisecond,
	})

	dataDir := filepath.Join(t.TempDir(), "eval_unreachable_store")

	runnerOpts := RunnerOptions{
		Config:    &cfg,
		Client:    client,
		CorpusDir: corpusDir,
		DataDir:   dataDir,
	}

	_, err := LoadOrIndexCorpus(context.Background(), runnerOpts)
	if err == nil {
		t.Fatalf("expected LoadOrIndexCorpus to fail with unreachable Ollama and 0 chunks, but got nil error")
	}
	if !strings.Contains(err.Error(), "0 chunks indexed") {
		t.Errorf("expected error message to mention '0 chunks indexed', got %v", err)
	}
}
