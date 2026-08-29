package index

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hendrixx-RE/Vektix/internal/config"
	"github.com/Hendrixx-RE/Vektix/internal/ollama"
	"github.com/Hendrixx-RE/Vektix/internal/store"
)

func testIndexConfig(exts ...string) *config.IndexConfig {
	if len(exts) == 0 {
		exts = []string{".txt"}
	}
	return &config.IndexConfig{
		MaxFileSizeMB: 10,
		Extensions:    exts,
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// newMockEmbedServer answers /api/embed with one deterministic vector of dim
// floats per input text, and reports the size of every batch it receives to
// onBatch (if non-nil). It never applies a task prefix itself, matching
// internal/ollama's real endpoint, which is the layer responsible for that.
func newMockEmbedServer(t *testing.T, dim int, onBatch func(n int)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Input []string `json:"input"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &payload)
		if onBatch != nil {
			onBatch(len(payload.Input))
		}
		embeddings := make([][]float32, len(payload.Input))
		for i := range embeddings {
			vec := make([]float32, dim)
			for j := range vec {
				vec[j] = float32(i*dim+j+1) / 1000
			}
			embeddings[i] = vec
		}
		data, _ := json.Marshal(map[string]any{"embeddings": embeddings})
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	}))
}

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.NewPersistentDB(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return s
}

// TestPipeline_BatchedEmbeddingAndCounts indexes enough small files to force
// several /api/embed calls, and checks that batching stays within the
// documented 64-100 range (every call but the last is a full maxEmbedBatch)
// instead of degrading into one HTTP round trip per chunk.
func TestPipeline_BatchedEmbeddingAndCounts(t *testing.T) {
	root := t.TempDir()
	const n = 150
	for i := 0; i < n; i++ {
		mustWriteFile(t, filepath.Join(root, fmt.Sprintf("file-%03d.txt", i)),
			fmt.Sprintf("File number %d contains a short sentence for the vektix pipeline test suite.", i))
	}

	var (
		mu      sync.Mutex
		batches []int
	)
	ts := newMockEmbedServer(t, 4, func(sz int) {
		mu.Lock()
		batches = append(batches, sz)
		mu.Unlock()
	})
	defer ts.Close()

	client := ollama.NewClient(ollama.Options{Host: ts.URL, EmbedTimeout: 5 * time.Second})
	st := newTestStore(t)

	e := &Engine{
		Store:    st,
		Embedder: client,
		IndexCfg: testIndexConfig(),
		Identity: DefaultIdentity("mock-model"),
		DataDir:  t.TempDir(),
	}

	res, err := e.Run(context.Background(), []string{root}, ModeIndex)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if res.Scanned != n || res.Added != n || res.Indexed != n {
		t.Fatalf("counts: scanned=%d added=%d indexed=%d, want %d", res.Scanned, res.Added, res.Indexed, n)
	}
	if res.Chunks != n {
		t.Fatalf("expected %d chunks (1/file), got %d", n, res.Chunks)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(batches) < 2 {
		t.Fatalf("expected multiple embed batches for %d chunks, got %d calls: %v", n, len(batches), batches)
	}
	sum := 0
	for _, b := range batches {
		if b > maxEmbedBatch {
			t.Errorf("batch of %d exceeds max %d", b, maxEmbedBatch)
		}
		sum += b
	}
	if sum != n {
		t.Fatalf("batches sum to %d, want %d", sum, n)
	}
	for i, b := range batches[:len(batches)-1] {
		if b != maxEmbedBatch {
			t.Errorf("batch %d has size %d, want a full batch of %d", i, b, maxEmbedBatch)
		}
	}
}

// TestPipeline_MalformedPDFQuarantinedRunContinues proves a PDF that fails to
// parse is quarantined, not fatal: the rest of the run must still complete
// and index every other file.
func TestPipeline_MalformedPDFQuarantinedRunContinues(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "notes.txt"), "A perfectly normal text file that should index cleanly.")

	badPDF, err := os.ReadFile(filepath.Join("..", "parser", "testdata", "malformed.pdf"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "broken.pdf"), badPDF, 0644); err != nil {
		t.Fatal(err)
	}

	ts := newMockEmbedServer(t, 4, nil)
	defer ts.Close()
	client := ollama.NewClient(ollama.Options{Host: ts.URL, EmbedTimeout: 5 * time.Second})
	st := newTestStore(t)

	e := &Engine{
		Store:    st,
		Embedder: client,
		IndexCfg: testIndexConfig(".txt", ".pdf"),
		Identity: DefaultIdentity("mock-model"),
		DataDir:  t.TempDir(),
	}

	res, err := e.Run(context.Background(), []string{root}, ModeIndex)
	if err != nil {
		t.Fatalf("a quarantined file must not abort the run: %v", err)
	}

	if res.Scanned != 2 {
		t.Fatalf("expected 2 files scanned, got %d", res.Scanned)
	}
	if res.Indexed != 1 {
		t.Fatalf("expected exactly 1 file indexed (notes.txt), got %d", res.Indexed)
	}
	if len(res.Quarantined) != 1 {
		t.Fatalf("expected 1 quarantined file, got %d: %+v", len(res.Quarantined), res.Quarantined)
	}
	if filepath.Base(res.Quarantined[0].Path) != "broken.pdf" {
		t.Errorf("expected broken.pdf quarantined, got %s", res.Quarantined[0].Path)
	}
}

// TestPipeline_OrphanPurgeOnSync deletes an indexed file and confirms `sync`
// purges its chunks from both the store and the manifest, and keeps
// dir_counts consistent with what's actually left.
func TestPipeline_OrphanPurgeOnSync(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "keep.txt"), "This file will remain indexed across the sync.")
	toDelete := filepath.Join(root, "gone.txt")
	mustWriteFile(t, toDelete, "This file will be deleted before the sync runs.")

	ts := newMockEmbedServer(t, 4, nil)
	defer ts.Close()
	client := ollama.NewClient(ollama.Options{Host: ts.URL, EmbedTimeout: 5 * time.Second})
	st := newTestStore(t)
	dataDir := t.TempDir()

	newEngine := func() *Engine {
		return &Engine{
			Store:    st,
			Embedder: client,
			IndexCfg: testIndexConfig(),
			Identity: DefaultIdentity("mock-model"),
			DataDir:  dataDir,
		}
	}

	res1, err := newEngine().Run(context.Background(), []string{root}, ModeIndex)
	if err != nil {
		t.Fatalf("initial index: %v", err)
	}
	if res1.Indexed != 2 {
		t.Fatalf("expected 2 files indexed, got %d", res1.Indexed)
	}
	totalChunks := res1.Chunks

	toDeleteAbs, err := filepath.Abs(toDelete)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(toDelete); err != nil {
		t.Fatal(err)
	}

	res2, err := newEngine().Run(context.Background(), []string{root}, ModeSync)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if res2.Removed != 1 {
		t.Fatalf("expected 1 orphan file removed, got %d", res2.Removed)
	}
	if res2.RemovedChunks == 0 {
		t.Fatalf("expected removed chunks > 0")
	}
	if res2.Unchanged != 1 {
		t.Fatalf("expected keep.txt to be reported unchanged, got %d", res2.Unchanged)
	}

	if got, want := st.Count(), totalChunks-res2.RemovedChunks; got != want {
		t.Fatalf("store count = %d, want %d", got, want)
	}

	m, err := LoadManifest(ManifestPath(dataDir))
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if _, ok := m.Files[toDeleteAbs]; ok {
		t.Errorf("manifest still references deleted file %s", toDeleteAbs)
	}
	if m.DirCounts[""] != st.Count() {
		t.Errorf("dir_counts totals %d, want %d (store count)", m.DirCounts[""], st.Count())
	}
}

// TestPipeline_DryRunMatchesRealRunCounts checks that --dry-run reports the
// exact same scanned/added/updated/unchanged counts a real run would produce,
// and that it never touches the store or persists a manifest.
func TestPipeline_DryRunMatchesRealRunCounts(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 12; i++ {
		mustWriteFile(t, filepath.Join(root, fmt.Sprintf("f%02d.txt", i)),
			fmt.Sprintf("Dry run parity check file number %d.", i))
	}

	dataDir := t.TempDir()

	dry := &Engine{
		IndexCfg: testIndexConfig(),
		Identity: DefaultIdentity("mock-model"),
		DataDir:  dataDir,
		DryRun:   true,
	}
	dryRes, err := dry.Run(context.Background(), []string{root}, ModeIndex)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}

	if _, err := os.Stat(ManifestPath(dataDir)); !os.IsNotExist(err) {
		t.Fatalf("dry run must not persist a manifest, stat err = %v", err)
	}

	ts := newMockEmbedServer(t, 4, nil)
	defer ts.Close()
	client := ollama.NewClient(ollama.Options{Host: ts.URL, EmbedTimeout: 5 * time.Second})
	st := newTestStore(t)

	real := &Engine{
		Store:    st,
		Embedder: client,
		IndexCfg: testIndexConfig(),
		Identity: DefaultIdentity("mock-model"),
		DataDir:  dataDir,
	}
	realRes, err := real.Run(context.Background(), []string{root}, ModeIndex)
	if err != nil {
		t.Fatalf("real run: %v", err)
	}

	if dryRes.Scanned != realRes.Scanned || dryRes.Added != realRes.Added ||
		dryRes.Updated != realRes.Updated || dryRes.Unchanged != realRes.Unchanged {
		t.Fatalf("dry run counts %+v do not match real run counts %+v", dryRes, realRes)
	}
	if len(dryRes.Files) != realRes.Indexed {
		t.Fatalf("dry run listed %d files, real run indexed %d", len(dryRes.Files), realRes.Indexed)
	}
}

// TestPipeline_ManifestInvalidationRefuses checks that a manifest built under
// a different embedding_model is refused outright — no store or embed calls
// — with an error that names the exact reindex command.
func TestPipeline_ManifestInvalidationRefuses(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "a.txt"), "some content")

	dataDir := t.TempDir()
	old := &Manifest{
		EmbeddingModel: "old-model",
		Dim:            4,
		PrefixScheme:   PrefixScheme,
		ChunkerVersion: ChunkerVersion,
		Files:          map[string]FileMeta{},
		DirCounts:      map[string]int{},
	}
	if err := old.SaveManifest(ManifestPath(dataDir)); err != nil {
		t.Fatal(err)
	}

	var called int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&called, 1)
		t.Error("embed endpoint should not be called when the manifest is invalid")
	}))
	defer ts.Close()
	client := ollama.NewClient(ollama.Options{Host: ts.URL, EmbedTimeout: 5 * time.Second})
	st := newTestStore(t)

	e := &Engine{
		Store:    st,
		Embedder: client,
		IndexCfg: testIndexConfig(),
		Identity: DefaultIdentity("new-model"),
		DataDir:  dataDir,
	}

	_, err := e.Run(context.Background(), []string{root}, ModeIndex)
	if err == nil {
		t.Fatal("expected an error for mismatched manifest identity")
	}
	var invalid *InvalidIndexError
	if !errors.As(err, &invalid) {
		t.Fatalf("expected *InvalidIndexError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), ReindexCommand) {
		t.Errorf("error should mention %q, got %q", ReindexCommand, err.Error())
	}
	if atomic.LoadInt32(&called) != 0 {
		t.Errorf("embed endpoint was called %d times, want 0", called)
	}
}

// TestPipeline_CancellationStopsCleanly cancels the context while an embed
// call is in flight and checks Run returns promptly with context.Canceled
// instead of hanging — the pipeline's stages must all unwind without
// deadlocking on the bounded channels between them.
func TestPipeline_CancellationStopsCleanly(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 5; i++ {
		mustWriteFile(t, filepath.Join(root, fmt.Sprintf("f%d.txt", i)),
			fmt.Sprintf("Cancellation test file number %d with a bit of content in it.", i))
	}

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"embeddings": []}`))
	}))
	defer ts.Close()
	defer close(release)

	client := ollama.NewClient(ollama.Options{Host: ts.URL, EmbedTimeout: 30 * time.Second})
	st := newTestStore(t)

	e := &Engine{
		Store:       st,
		Embedder:    client,
		IndexCfg:    testIndexConfig(),
		Identity:    DefaultIdentity("mock-model"),
		DataDir:     t.TempDir(),
		ChannelSize: 1,
		EmbedBatch:  2,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-started
		cancel()
	}()

	done := make(chan struct{})
	var runErr error
	go func() {
		_, runErr = e.Run(ctx, []string{root}, ModeIndex)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancellation — possible deadlock or goroutine leak")
	}

	if !errors.Is(runErr, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", runErr)
	}
}

// TestPipeline_BoundedChannelsNoDeadlock forces every stage's channel down to
// a single slot, so the walker is guaranteed to block waiting for downstream
// stages to catch up rather than buffering the whole tree in memory. A tiny
// misstep in the shutdown/backpressure wiring shows up as a hang here.
func TestPipeline_BoundedChannelsNoDeadlock(t *testing.T) {
	root := t.TempDir()
	const n = 40
	for i := 0; i < n; i++ {
		mustWriteFile(t, filepath.Join(root, fmt.Sprintf("f%02d.txt", i)),
			fmt.Sprintf("Backpressure test file number %d.", i))
	}

	ts := newMockEmbedServer(t, 4, nil)
	defer ts.Close()
	client := ollama.NewClient(ollama.Options{Host: ts.URL, EmbedTimeout: 5 * time.Second})
	st := newTestStore(t)

	e := &Engine{
		Store:        st,
		Embedder:     client,
		IndexCfg:     testIndexConfig(),
		Identity:     DefaultIdentity("mock-model"),
		DataDir:      t.TempDir(),
		ChannelSize:  1,
		EmbedBatch:   2,
		ParseWorkers: 2,
		ChunkWorkers: 2,
	}

	done := make(chan struct{})
	var res *Result
	var runErr error
	go func() {
		res, runErr = e.Run(context.Background(), []string{root}, ModeIndex)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not complete — bounded channels likely deadlocked")
	}

	if runErr != nil {
		t.Fatalf("Run: %v", runErr)
	}
	if res.Scanned != n || res.Indexed != n || res.Chunks != n {
		t.Fatalf("counts: scanned=%d indexed=%d chunks=%d, want %d", res.Scanned, res.Indexed, res.Chunks, n)
	}
}

// TestPipeline_ReindexRebuildsFromScratch tests that ModeReindex purges
// previous chunks and adopts a new model identity.
func TestPipeline_ReindexRebuildsFromScratch(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "doc.txt"), "Testing reindex functionality from scratch.")

	ts := newMockEmbedServer(t, 4, nil)
	defer ts.Close()
	client := ollama.NewClient(ollama.Options{Host: ts.URL, EmbedTimeout: 5 * time.Second})
	st := newTestStore(t)
	dataDir := t.TempDir()

	e1 := &Engine{
		Store:    st,
		Embedder: client,
		IndexCfg: testIndexConfig(),
		Identity: DefaultIdentity("model-v1"),
		DataDir:  dataDir,
	}

	res1, err := e1.Run(context.Background(), []string{root}, ModeIndex)
	if err != nil {
		t.Fatalf("initial index: %v", err)
	}
	if res1.Indexed != 1 {
		t.Fatalf("expected 1 file indexed, got %d", res1.Indexed)
	}

	// Now run reindex with a new model
	e2 := &Engine{
		Store:    st,
		Embedder: client,
		IndexCfg: testIndexConfig(),
		Identity: DefaultIdentity("model-v2"),
		DataDir:  dataDir,
	}

	res2, err := e2.Run(context.Background(), []string{root}, ModeReindex)
	if err != nil {
		t.Fatalf("reindex: %v", err)
	}
	if res2.Removed != 1 {
		t.Errorf("expected 1 file removed during reindex, got %d", res2.Removed)
	}
	if res2.Indexed != 1 {
		t.Errorf("expected 1 file re-indexed, got %d", res2.Indexed)
	}

	m, err := LoadManifest(ManifestPath(dataDir))
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if m.EmbeddingModel != "model-v2" {
		t.Errorf("expected manifest model 'model-v2', got %s", m.EmbeddingModel)
	}
}

// TestPipeline_FileUpdateReplacesChunks tests that modifying a file replaces
// its old chunks cleanly.
func TestPipeline_FileUpdateReplacesChunks(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "update.txt")
	mustWriteFile(t, filePath, "Initial version of file with some words.")

	ts := newMockEmbedServer(t, 4, nil)
	defer ts.Close()
	client := ollama.NewClient(ollama.Options{Host: ts.URL, EmbedTimeout: 5 * time.Second})
	st := newTestStore(t)
	dataDir := t.TempDir()

	e := &Engine{
		Store:    st,
		Embedder: client,
		IndexCfg: testIndexConfig(),
		Identity: DefaultIdentity("mock-model"),
		DataDir:  dataDir,
	}

	res1, err := e.Run(context.Background(), []string{root}, ModeIndex)
	if err != nil {
		t.Fatalf("initial run: %v", err)
	}
	if res1.Added != 1 {
		t.Fatalf("expected 1 added, got %d", res1.Added)
	}

	// Update the file content and mtime
	time.Sleep(10 * time.Millisecond)
	mustWriteFile(t, filePath, "Updated version with completely different content.")

	res2, err := e.Run(context.Background(), []string{root}, ModeIndex)
	if err != nil {
		t.Fatalf("update run: %v", err)
	}
	if res2.Updated != 1 {
		t.Fatalf("expected 1 updated, got %d", res2.Updated)
	}
	if res2.Unchanged != 0 {
		t.Fatalf("expected 0 unchanged, got %d", res2.Unchanged)
	}
}

// TestPipeline_MultipleRoots tests indexing across multiple directories.
func TestPipeline_MultipleRoots(t *testing.T) {
	root1 := t.TempDir()
	root2 := t.TempDir()
	mustWriteFile(t, filepath.Join(root1, "r1.txt"), "Root 1 file content.")
	mustWriteFile(t, filepath.Join(root2, "r2.txt"), "Root 2 file content.")

	ts := newMockEmbedServer(t, 4, nil)
	defer ts.Close()
	client := ollama.NewClient(ollama.Options{Host: ts.URL, EmbedTimeout: 5 * time.Second})
	st := newTestStore(t)
	dataDir := t.TempDir()

	e := &Engine{
		Store:    st,
		Embedder: client,
		IndexCfg: testIndexConfig(),
		Identity: DefaultIdentity("mock-model"),
		DataDir:  dataDir,
	}

	res, err := e.Run(context.Background(), []string{root1, root2}, ModeIndex)
	if err != nil {
		t.Fatalf("multi-root run: %v", err)
	}
	if res.Scanned != 2 || res.Indexed != 2 {
		t.Fatalf("expected 2 scanned and indexed, got %d / %d", res.Scanned, res.Indexed)
	}

	m, err := LoadManifest(ManifestPath(dataDir))
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if len(m.Roots) != 2 {
		t.Errorf("expected 2 roots in manifest, got %d", len(m.Roots))
	}
}
