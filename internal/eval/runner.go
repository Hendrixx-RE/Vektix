package eval

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Hendrixx-RE/Vektix/internal/config"
	"github.com/Hendrixx-RE/Vektix/internal/excerpt"
	"github.com/Hendrixx-RE/Vektix/internal/index"
	"github.com/Hendrixx-RE/Vektix/internal/ollama"
	"github.com/Hendrixx-RE/Vektix/internal/resolve"
	"github.com/Hendrixx-RE/Vektix/internal/router"
	"github.com/Hendrixx-RE/Vektix/internal/store"
)

// DatasetType indicates whether a dataset is for intent classification or locate evaluation.
type DatasetType string

const (
	DatasetTypeUnknown DatasetType = "unknown"
	DatasetTypeIntent  DatasetType = "intent"
	DatasetTypeLocate  DatasetType = "locate"
)

// DetectDatasetType inspects the first non-empty JSON line of a dataset to determine its schema.
func DetectDatasetType(datasetPath string) (DatasetType, error) {
	f, err := os.Open(datasetPath)
	if err != nil {
		return DatasetTypeUnknown, fmt.Errorf("opening dataset: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}

		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			return DatasetTypeUnknown, fmt.Errorf("parsing dataset line: %w", err)
		}

		if _, hasExpected := raw["expected"]; hasExpected {
			return DatasetTypeIntent, nil
		}
		if _, hasExpectPath := raw["expect_path"]; hasExpectPath {
			return DatasetTypeLocate, nil
		}
		if _, hasTier := raw["tier"]; hasTier {
			return DatasetTypeIntent, nil
		}
		return DatasetTypeUnknown, nil
	}

	if err := scanner.Err(); err != nil {
		return DatasetTypeUnknown, fmt.Errorf("reading dataset: %w", err)
	}

	return DatasetTypeUnknown, errors.New("empty dataset")
}

// RunnerOptions configures the evaluation harness.
type RunnerOptions struct {
	Config     *config.Config
	Client     *ollama.Client
	CorpusDir  string
	DataDir    string
	Limit      int
	RRFK       int
	MinArms    int
	MaxLines   int
	ProgressFn func(completed, total int)
}

// CorpusIndex contains loaded in-memory search structures for evaluation.
type CorpusIndex struct {
	Manifest  *index.Manifest
	Store     *store.Store
	Chunks    []store.Chunk
	Paths     *resolve.PathIndex
	BM25      *resolve.BM25Index
	Vector    *resolve.VectorArm
	CorpusDir string
	DataDir   string
}

// LoadOrIndexCorpus ensures the target corpus is indexed and loads the search arms.
func LoadOrIndexCorpus(ctx context.Context, opts RunnerOptions) (*CorpusIndex, error) {
	cfg := opts.Config
	if cfg == nil {
		defaultCfg := config.DefaultConfig()
		cfg = &defaultCfg
	}

	corpusDir := opts.CorpusDir
	if corpusDir == "" {
		candidates := []string{"testdata/corpus", "../testdata/corpus", "../../testdata/corpus"}
		for _, c := range candidates {
			if info, err := os.Stat(c); err == nil && info.IsDir() {
				corpusDir = c
				break
			}
		}
		if corpusDir == "" {
			return nil, errors.New("corpus directory not found; specify --corpus")
		}
	}

	absCorpusDir, err := filepath.Abs(corpusDir)
	if err != nil {
		return nil, fmt.Errorf("resolving corpus dir: %w", err)
	}

	dataDir := opts.DataDir
	if dataDir == "" {
		expandedDataDir, err := config.ExpandPath(cfg.General.DataDir)
		if err == nil {
			dataDir = filepath.Join(expandedDataDir, "eval_corpus")
		} else {
			dataDir = filepath.Join(os.TempDir(), "vektix_eval_corpus")
		}
	}

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("creating eval data dir %s: %w", dataDir, err)
	}

	db, err := store.NewPersistentDB(index.StorePath(dataDir))
	if err != nil {
		return nil, fmt.Errorf("opening store at %s: %w", dataDir, err)
	}

	client := opts.Client
	if client == nil {
		client = ollama.NewClient(ollama.Options{
			Host:              cfg.Ollama.Host,
			EmbedTimeout:      time.Duration(cfg.Ollama.Timeouts.EmbedBatchSeconds) * time.Second,
			IntentTimeout:     time.Duration(cfg.Ollama.Timeouts.IntentSeconds) * time.Second,
			StreamIdleTimeout: time.Duration(cfg.Ollama.Timeouts.StreamIdleSeconds) * time.Second,
		})
	}

	manifestPath := index.ManifestPath(dataDir)
	engine := index.NewEngine(cfg, db, client, dataDir)
	_, runErr := engine.Run(ctx, []string{absCorpusDir}, index.ModeSync)
	if runErr != nil {
		var invalid *index.InvalidIndexError
		if errors.As(runErr, &invalid) {
			return nil, runErr
		}
	}

	m, err := index.LoadManifest(manifestPath)
	if err != nil {
		if runErr != nil {
			return nil, fmt.Errorf("indexing evaluation corpus at %s failed: %w", absCorpusDir, runErr)
		}
		return nil, fmt.Errorf("loading manifest from %s: %w", manifestPath, err)
	}

	// Deterministic chunk loading
	files := make([]string, 0, len(m.Files))
	for path := range m.Files {
		files = append(files, path)
	}
	sort.Strings(files)

	var chunks []store.Chunk
	for _, path := range files {
		for _, id := range m.Files[path].Chunks {
			chunk, err := db.GetByID(ctx, id)
			if err != nil {
				continue
			}
			if chunk.Path == "" {
				chunk.Path = path
			}
			chunks = append(chunks, chunk)
		}
	}

	if len(chunks) == 0 {
		if runErr != nil {
			return nil, fmt.Errorf("indexing evaluation corpus at %s failed: %w (0 chunks indexed; ensure Ollama is running with %s at %s)",
				absCorpusDir, runErr, cfg.Ollama.EmbeddingModel, cfg.Ollama.Host)
		}
		return nil, fmt.Errorf("evaluation corpus at %s contains 0 indexed chunks", absCorpusDir)
	}

	if runErr != nil {
		fmt.Fprintf(os.Stderr, "warning: indexing evaluation corpus at %s encountered an error: %v (evaluating against partial index of %d files, %d chunks)\n",
			absCorpusDir, runErr, len(m.Files), len(chunks))
	}

	pathIndex := resolve.NewPathIndex(chunks)
	bm25Index := resolve.NewBM25Index(chunks)
	vectorArm := resolve.NewVectorArm(
		db,
		client,
		ollama.NewEmbeddingCache(256),
		m,
		cfg.Ollama.EmbeddingModel,
		cfg.Ollama.KeepAlive,
		cfg.Search.OversampleFloor,
	)

	return &CorpusIndex{
		Manifest:  m,
		Store:     db,
		Chunks:    chunks,
		Paths:     pathIndex,
		BM25:      bm25Index,
		Vector:    vectorArm,
		CorpusDir: absCorpusDir,
		DataDir:   dataDir,
	}, nil
}

// LoadIntentCases reads an intent dataset from a JSONL file or reader.
func LoadIntentCases(r io.Reader) ([]IntentCase, error) {
	var cases []IntentCase
	scanner := bufio.NewScanner(r)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		var tc IntentCase
		if err := json.Unmarshal([]byte(line), &tc); err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNum, err)
		}
		cases = append(cases, tc)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return cases, nil
}

// RunIntent executes intent classification evaluation across all test cases.
func RunIntent(ctx context.Context, datasetPath string, opts RunnerOptions) (*IntentMetrics, []IntentCaseResult, error) {
	f, err := os.Open(datasetPath)
	if err != nil {
		return nil, nil, fmt.Errorf("opening intent dataset: %w", err)
	}
	defer f.Close()

	cases, err := LoadIntentCases(f)
	if err != nil {
		return nil, nil, fmt.Errorf("loading intent cases: %w", err)
	}

	cfg := opts.Config
	if cfg == nil {
		defaultCfg := config.DefaultConfig()
		cfg = &defaultCfg
	}

	client := opts.Client
	if client == nil {
		client = ollama.NewClient(ollama.Options{
			Host:              cfg.Ollama.Host,
			IntentTimeout:     time.Duration(cfg.Ollama.Timeouts.IntentSeconds) * time.Second,
			EmbedTimeout:      time.Duration(cfg.Ollama.Timeouts.EmbedBatchSeconds) * time.Second,
			StreamIdleTimeout: time.Duration(cfg.Ollama.Timeouts.StreamIdleSeconds) * time.Second,
		})
	}

	results := make([]IntentCaseResult, 0, len(cases))
	for i, tc := range cases {
		start := time.Now()
		actual := router.ParseFastPath(tc.Input)
		actualTier := 1
		var errStr string

		if actual == nil {
			actualTier = 2
			var llmErr error
			actual, llmErr = router.ParseLLM(ctx, client, cfg.Ollama.IntentModel, tc.Input)
			if llmErr != nil {
				errStr = llmErr.Error()
			}
		}
		lat := time.Since(start)

		actionOK := (actual != nil && tc.Expected != nil && actual.Action == tc.Expected.Action)
		paramsOK := (actual != nil && tc.Expected != nil && CheckIntentParams(tc.Expected, actual))
		tierOK := (actualTier == tc.Tier)

		results = append(results, IntentCaseResult{
			Case:       tc,
			Actual:     actual,
			ActualTier: actualTier,
			ActionOK:   actionOK,
			ParamsOK:   paramsOK,
			TierOK:     tierOK,
			Latency:    lat,
			Error:      errStr,
		})

		if opts.ProgressFn != nil {
			opts.ProgressFn(i+1, len(cases))
		}
	}

	metrics := ComputeIntentMetrics(results)
	return &metrics, results, nil
}

// LoadLocateCases reads a locate dataset from a JSONL file or reader.
func LoadLocateCases(r io.Reader) ([]LocateCase, error) {
	var cases []LocateCase
	scanner := bufio.NewScanner(r)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		var tc LocateCase
		if err := json.Unmarshal([]byte(line), &tc); err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNum, err)
		}
		cases = append(cases, tc)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return cases, nil
}

// RunLocate executes retrieval and ranking evaluation across all locate cases against the indexed corpus.
func RunLocate(ctx context.Context, corpus *CorpusIndex, datasetPath string, opts RunnerOptions) (*LocateMetrics, []LocateCaseResult, error) {
	f, err := os.Open(datasetPath)
	if err != nil {
		return nil, nil, fmt.Errorf("opening locate dataset: %w", err)
	}
	defer f.Close()

	cases, err := LoadLocateCases(f)
	if err != nil {
		return nil, nil, fmt.Errorf("loading locate cases: %w", err)
	}

	rrfK := opts.RRFK
	if rrfK <= 0 {
		if opts.Config != nil && opts.Config.Search.RRFK > 0 {
			rrfK = opts.Config.Search.RRFK
		} else {
			rrfK = 60
		}
	}

	minArms := opts.MinArms
	if minArms <= 0 {
		if opts.Config != nil && opts.Config.Search.MinArms > 0 {
			minArms = opts.Config.Search.MinArms
		} else {
			minArms = 1
		}
	}

	maxLines := opts.MaxLines
	if maxLines <= 0 {
		maxLines = 12
	}

	vecK := 20
	results := make([]LocateCaseResult, 0, len(cases))

	for i, tc := range cases {
		start := time.Now()
		var warnings []string

		// 1. Global retrieval
		pathHits := corpus.Paths.Search(tc.Query, "")
		bm25Hits := corpus.BM25.Search(tc.Query, "")
		vecHits, vecErr := corpus.Vector.Search(ctx, tc.Query, "", vecK)
		if vecErr != nil {
			warnings = append(warnings, vecErr.Error())
		}

		globalLists := []resolve.ResultList{pathHits, bm25Hits}
		if vecErr == nil && len(vecHits) > 0 {
			globalLists = append(globalLists, vecHits)
		}

		fusedGlobal := resolve.Fuse(globalLists, rrfK, minArms, 500)
		globalTopPaths := dedupePaths(fusedGlobal)

		globalHitAt1 := checkPathListHit(globalTopPaths, tc.ExpectPath, 1)
		globalHitAt3 := checkPathListHit(globalTopPaths, tc.ExpectPath, 3)

		// 2. Scoped retrieval
		scopedDir := resolveScopeDir(corpus.CorpusDir, tc.Scope)
		pathScopedHits := corpus.Paths.Search(tc.Query, scopedDir)
		bm25ScopedHits := corpus.BM25.Search(tc.Query, scopedDir)
		vecScopedHits, _ := corpus.Vector.Search(ctx, tc.Query, scopedDir, vecK)

		scopedLists := []resolve.ResultList{pathScopedHits, bm25ScopedHits}
		if vecErr == nil && len(vecScopedHits) > 0 {
			scopedLists = append(scopedLists, vecScopedHits)
		}

		fusedScoped := resolve.Fuse(scopedLists, rrfK, minArms, 500)
		scopedTopPaths := dedupePaths(fusedScoped)

		scopedHitAt1 := checkPathListHit(scopedTopPaths, tc.ExpectPath, 1)
		scopedHitAt3 := checkPathListHit(scopedTopPaths, tc.ExpectPath, 3)

		// 3. Ablation checks (top 3)
		bm25HitAt3 := checkChunkListHit(bm25Hits, tc.ExpectPath, 3)
		vectorHitAt3 := false
		if vecErr == nil {
			vectorHitAt3 = checkChunkListHit(vecHits, tc.ExpectPath, 3)
		}
		rrfHitAt3 := globalHitAt3

		// 4. Excerpt correctness check
		excerptOK := true
		if tc.ExpectedText != "" {
			excerptOK = false
			candidates := fusedGlobal
			if tc.Scope != "" && !strings.EqualFold(tc.Scope, "global") && len(fusedScoped) > 0 {
				candidates = fusedScoped
			}
			for i, sc := range candidates {
				if i >= 3 {
					break
				}
				if CheckPathMatch(sc.Path, tc.ExpectPath) {
					if source, readErr := os.ReadFile(sc.Path); readErr == nil {
						text, _ := excerpt.Expand(sc.Chunk, source, excerpt.ExpandConfig{MaxLines: maxLines})
						if CheckExcerptCorrectness(text, tc.ExpectedText) {
							excerptOK = true
							break
						}
					}
				}
			}
		}

		latency := time.Since(start)

		results = append(results, LocateCaseResult{
			Case:           tc,
			GlobalHitAt1:   globalHitAt1,
			GlobalHitAt3:   globalHitAt3,
			ScopedHitAt1:   scopedHitAt1,
			ScopedHitAt3:   scopedHitAt3,
			BM25HitAt3:     bm25HitAt3,
			VectorHitAt3:   vectorHitAt3,
			RRFHitAt3:      rrfHitAt3,
			ExcerptOK:      excerptOK,
			Latency:        latency,
			GlobalTopPaths: globalTopPaths,
			ScopedTopPaths: scopedTopPaths,
			Warnings:       warnings,
		})

		if opts.ProgressFn != nil {
			opts.ProgressFn(i+1, len(cases))
		}
	}

	metrics := ComputeLocateMetrics(results)
	return &metrics, results, nil
}

func dedupePaths(results []resolve.ScoredChunk) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range results {
		if seen[r.Path] {
			continue
		}
		seen[r.Path] = true
		out = append(out, r.Path)
	}
	return out
}

func checkPathListHit(paths []string, expectPath string, k int) bool {
	limit := k
	if limit > len(paths) {
		limit = len(paths)
	}
	for i := 0; i < limit; i++ {
		if CheckPathMatch(paths[i], expectPath) {
			return true
		}
	}
	return false
}

func checkChunkListHit(chunks resolve.ResultList, expectPath string, k int) bool {
	seen := map[string]bool{}
	count := 0
	for _, ch := range chunks {
		if seen[ch.Path] {
			continue
		}
		seen[ch.Path] = true
		count++
		if CheckPathMatch(ch.Path, expectPath) {
			return true
		}
		if count >= k {
			break
		}
	}
	return false
}

func resolveScopeDir(corpusDir, scope string) string {
	scope = strings.TrimSpace(scope)
	if scope == "" || strings.EqualFold(scope, "global") {
		return ""
	}
	if filepath.IsAbs(scope) {
		return filepath.Clean(scope)
	}
	if strings.HasPrefix(scope, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Clean(filepath.Join(home, scope[2:]))
		}
	}
	return filepath.Clean(filepath.Join(corpusDir, scope))
}

// EncodeJSONLines encodes items as JSON Lines.
func EncodeJSONLines[T any](w io.Writer, items []T) error {
	buf := new(bytes.Buffer)
	for _, item := range items {
		data, err := json.Marshal(item)
		if err != nil {
			return err
		}
		buf.Write(data)
		buf.WriteByte('\n')
	}
	_, err := w.Write(buf.Bytes())
	return err
}
