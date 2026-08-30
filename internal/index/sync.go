// Package index implements the walk → parse → chunk → embed → store pipeline
// described in plan.md ("Indexing & Freshness"), plus the reconciliation pass
// that keeps the manifest and the vector store in agreement.
package index

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Hendrixx-RE/Vektix/internal/chunker"
	"github.com/Hendrixx-RE/Vektix/internal/config"
	"github.com/Hendrixx-RE/Vektix/internal/format"
	"github.com/Hendrixx-RE/Vektix/internal/ollama"
	"github.com/Hendrixx-RE/Vektix/internal/parser"
	"github.com/Hendrixx-RE/Vektix/internal/store"
)

const (
	// ChunkerVersion identifies the chunking algorithm. Bumping it invalidates
	// every existing index, because chunk boundaries (and therefore locators)
	// would no longer match what is stored.
	ChunkerVersion = 3

	// PrefixScheme identifies the embedding task-prefix scheme applied by
	// internal/ollama (search_document: / search_query:). The indexer never
	// applies the prefix itself.
	PrefixScheme = "nomic-v1.5"

	// ReindexCommand is printed verbatim whenever the index is refused.
	ReindexCommand = "vektix reindex"

	// maxEmbedBatch is the batch size used per /api/embed call, within the
	// 64-100 range plan.md specifies. Only the final batch of a run may be
	// smaller than this, once every remaining chunk has been accumulated.
	maxEmbedBatch = 100

	defaultChannelSize = 256
	defaultPDFTimeout  = 30 * time.Second
)

// Mode selects what a run does with files that have disappeared from disk.
type Mode int

const (
	// ModeIndex adds and updates files under the given roots. It never deletes.
	ModeIndex Mode = iota
	// ModeSync re-walks the roots and reconciles: adds, updates, and purges
	// the chunks of files that no longer exist or are no longer eligible.
	ModeSync
	// ModeReindex drops every chunk under the roots and rebuilds from scratch.
	// It is the only mode that may run against a manifest whose identity no
	// longer matches the current configuration.
	ModeReindex
)

func (m Mode) String() string {
	switch m {
	case ModeSync:
		return "sync"
	case ModeReindex:
		return "reindex"
	default:
		return "index"
	}
}

// VectorStore is the subset of *store.Store the indexer needs.
type VectorStore interface {
	AddDocuments(ctx context.Context, chunks []store.Chunk) error
	Delete(ctx context.Context, where, whereDocument map[string]string, ids ...string) error
}

// Embedder is the subset of *ollama.Client the indexer needs.
type Embedder interface {
	Embed(ctx context.Context, req ollama.EmbedRequest) (*ollama.EmbedResponse, error)
}

// Identity is the set of parameters that, when changed, make every stored
// vector incomparable with a freshly embedded one.
type Identity struct {
	EmbeddingModel string
	Dim            int // 0 means "adopt whatever the embedding endpoint returns"
	PrefixScheme   string
	ChunkerVersion int
}

// DefaultIdentity returns the identity implied by this build plus the
// configured embedding model. Dim is left at 0 and learned on the first batch.
func DefaultIdentity(model string) Identity {
	return Identity{
		EmbeddingModel: model,
		PrefixScheme:   PrefixScheme,
		ChunkerVersion: ChunkerVersion,
	}
}

// InvalidIndexError reports a manifest whose identity no longer matches the
// current configuration. Vektix refuses to proceed rather than mixing
// incompatible vectors into one collection.
type InvalidIndexError struct {
	Reasons []string
}

func (e *InvalidIndexError) Error() string {
	return fmt.Sprintf("index built with different settings (%s); refusing to proceed — rebuild with: %s",
		strings.Join(e.Reasons, "; "), ReindexCommand)
}

// Is lets callers match with errors.Is(err, ErrManifestMismatch).
func (e *InvalidIndexError) Is(target error) bool { return target == ErrManifestMismatch }

// QuarantineEntry records a file that could not be indexed. A quarantined file
// never aborts a run; the list is persisted for `vektix status` to surface.
type QuarantineEntry struct {
	Path   string    `json:"path"`
	Reason string    `json:"reason"`
	Time   time.Time `json:"time"`
}

// Progress is reported as the pipeline advances, so long runs are not silent.
type Progress struct {
	Scanned     int
	Unchanged   int
	Indexed     int
	Chunks      int
	Quarantined int
}

// Result is the summary of a run, matching the output documented in plan.md.
type Result struct {
	Scanned       int
	Unchanged     int
	Updated       int
	Added         int
	Removed       int
	RemovedChunks int
	Indexed       int // files actually written to the store
	Chunks        int // chunks written to the store
	Quarantined   []QuarantineEntry
	Files         []string // dry-run only: what WOULD be indexed
	Elapsed       time.Duration
}

// StorePath returns the vector store directory inside the data dir.
func StorePath(dataDir string) string { return filepath.Join(dataDir, "store") }

// ManifestPath returns the manifest file inside the data dir.
func ManifestPath(dataDir string) string { return filepath.Join(dataDir, "manifest.json") }

// QuarantinePath returns the quarantine file inside the data dir.
func QuarantinePath(dataDir string) string { return filepath.Join(dataDir, "quarantine.json") }

// LoadQuarantine reads the quarantine list. A missing file is not an error.
func LoadQuarantine(path string) ([]QuarantineEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var entries []QuarantineEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// SaveQuarantine writes the quarantine list, removing the file when empty.
func SaveQuarantine(path string, entries []QuarantineEntry) error {
	if len(entries) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Engine runs the indexing pipeline.
type Engine struct {
	Store       VectorStore
	Embedder    Embedder
	IndexCfg    *config.IndexConfig
	ChunkingCfg *config.ChunkingConfig
	Identity    Identity

	// DataDir holds the manifest and quarantine list.
	DataDir string

	// KeepAlive is forwarded to Ollama so the embedding model is not evicted
	// between batches.
	KeepAlive string

	// DryRun walks and classifies files but performs no parsing, embedding,
	// storing, or deletion. Counts are produced by the same walk code a real
	// run uses, so they match.
	DryRun bool

	// Transient marks chunks and file metadata as ephemeral/transient so they can
	// be expired by vektix sync on an LRU basis.
	Transient bool

	ParseWorkers int
	ChunkWorkers int
	EmbedBatch   int
	ChannelSize  int
	PDFTimeout   time.Duration

	OnProgress func(Progress)
}

// NewEngine builds an Engine from the loaded configuration.
func NewEngine(cfg *config.Config, st VectorStore, emb Embedder, dataDir string) *Engine {
	idxCfg := cfg.Index
	chunkCfg := cfg.Chunking
	return &Engine{
		Store:       st,
		Embedder:    emb,
		IndexCfg:    &idxCfg,
		ChunkingCfg: &chunkCfg,
		Identity:    DefaultIdentity(cfg.Ollama.EmbeddingModel),
		DataDir:     dataDir,
		KeepAlive:   cfg.Ollama.KeepAlive,
	}
}

func (e *Engine) applyDefaults() {
	if e.ParseWorkers <= 0 {
		e.ParseWorkers = clampWorkers(runtime.NumCPU())
	}
	if e.ChunkWorkers <= 0 {
		e.ChunkWorkers = clampWorkers(runtime.NumCPU())
	}
	if e.EmbedBatch <= 0 {
		e.EmbedBatch = maxEmbedBatch
	}
	if e.EmbedBatch > maxEmbedBatch {
		e.EmbedBatch = maxEmbedBatch
	}
	if e.ChannelSize <= 0 {
		e.ChannelSize = defaultChannelSize
	}
	if e.PDFTimeout <= 0 {
		e.PDFTimeout = defaultPDFTimeout
	}
	if e.Identity.PrefixScheme == "" {
		e.Identity.PrefixScheme = PrefixScheme
	}
	if e.Identity.ChunkerVersion == 0 {
		e.Identity.ChunkerVersion = ChunkerVersion
	}
	if e.ChunkingCfg == nil {
		defaultChunking := config.DefaultConfig().Chunking
		e.ChunkingCfg = &defaultChunking
	}
}

func clampWorkers(n int) int {
	if n < 2 {
		return 2
	}
	if n > 8 {
		return 8
	}
	return n
}

// Run executes a full pipeline pass over roots.
//
// Cancelling ctx stops every stage and still persists the manifest, which is
// only advanced for files whose chunks are already in the store — so an
// interrupted run leaves a smaller index, never an inconsistent one.
func (e *Engine) Run(ctx context.Context, roots []string, mode Mode) (*Result, error) {
	start := time.Now()
	e.applyDefaults()

	if e.IndexCfg == nil {
		return nil, errors.New("index: no index configuration")
	}
	if !e.DryRun && (e.Store == nil || e.Embedder == nil) {
		return nil, errors.New("index: store and embedder are required")
	}

	absRoots, err := absolutePaths(roots)
	if err != nil {
		return nil, err
	}

	m, existed, err := e.loadManifest()
	if err != nil {
		return nil, err
	}
	if existed && mode != ModeReindex {
		if err := checkIdentity(m, e.Identity); err != nil {
			return nil, err
		}
	}

	res := &Result{}

	if mode == ModeReindex {
		if len(absRoots) == 0 {
			absRoots = manifestRoots(m)
		}
		if !e.DryRun {
			files, chunks, err := e.purge(ctx, m, absRoots)
			if err != nil {
				return nil, err
			}
			res.Removed, res.RemovedChunks = files, chunks
		}
		stampIdentity(m, e.Identity)
	}
	if len(absRoots) == 0 {
		absRoots = manifestRoots(m)
	}
	if len(absRoots) == 0 {
		return nil, errors.New("index: no roots to index")
	}
	if !existed {
		stampIdentity(m, e.Identity)
	}

	p := newPipeline(e, m, res)
	runErr := p.run(ctx, absRoots)

	// Reconciliation. Only sync purges orphans: `index <path>` is additive by
	// definition, and a partial (cancelled) walk must never be mistaken for
	// "these files are gone".
	if runErr == nil && mode != ModeIndex {
		removed, removedChunks, err := e.reconcile(ctx, m, absRoots, p.seen)
		res.Removed += removed
		res.RemovedChunks += removedChunks
		if err != nil && runErr == nil {
			runErr = err
		}

		if mode == ModeSync {
			if err := e.expireTransientRoots(ctx, m, res); err != nil && runErr == nil {
				runErr = err
			}
		}
	}

	res.Quarantined = p.snapshotQuarantine()
	res.Elapsed = time.Since(start)

	if e.DryRun {
		sort.Strings(res.Files)
		return res, runErr
	}

	m.Roots = mergeRoots(m.Roots, absRoots)
	if e.Transient {
		if m.TransientRoots == nil {
			m.TransientRoots = make(map[string]int64)
		}
		for _, r := range absRoots {
			m.TransientRoots[r] = time.Now().Unix()
		}
	} else if mode == ModeIndex {
		for _, r := range absRoots {
			delete(m.TransientRoots, r)
		}
	}

	if err := e.persist(m, res.Quarantined); err != nil && runErr == nil {
		runErr = err
	}
	return res, runErr
}

func (e *Engine) loadManifest() (*Manifest, bool, error) {
	m, err := LoadManifest(ManifestPath(e.DataDir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Manifest{
				Files:          make(map[string]FileMeta),
				DirCounts:      make(map[string]int),
				TransientRoots: make(map[string]int64),
			}, false, nil
		}
		return nil, false, err
	}
	if m.TransientRoots == nil {
		m.TransientRoots = make(map[string]int64)
	}
	return m, true, nil
}

func (e *Engine) persist(m *Manifest, quarantined []QuarantineEntry) error {
	if err := os.MkdirAll(e.DataDir, 0755); err != nil {
		return err
	}
	if err := m.SaveManifest(ManifestPath(e.DataDir)); err != nil {
		return err
	}
	return SaveQuarantine(QuarantinePath(e.DataDir), quarantined)
}

func stampIdentity(m *Manifest, id Identity) {
	m.EmbeddingModel = id.EmbeddingModel
	m.PrefixScheme = id.PrefixScheme
	m.ChunkerVersion = id.ChunkerVersion
	if id.Dim > 0 {
		m.Dim = id.Dim
	}
	if m.Files == nil {
		m.Files = make(map[string]FileMeta)
	}
	if m.DirCounts == nil {
		m.DirCounts = make(map[string]int)
	}
	if m.TransientRoots == nil {
		m.TransientRoots = make(map[string]int64)
	}
}

// checkIdentity enforces Manifest.CheckValidity and explains exactly which
// field moved, so the user is never told to reindex without a reason.
func checkIdentity(m *Manifest, want Identity) error {
	// Dim is learned from the embedding endpoint, so an unset expectation
	// defers to the manifest; the runtime check in the embed stage still
	// catches a model that silently changed dimensions.
	dim := want.Dim
	if dim == 0 {
		dim = m.Dim
	}
	if err := m.CheckValidity(want.EmbeddingModel, dim, want.PrefixScheme, want.ChunkerVersion); err == nil {
		return nil
	}

	var reasons []string
	if m.EmbeddingModel != want.EmbeddingModel {
		reasons = append(reasons, fmt.Sprintf("embedding_model %q -> %q", m.EmbeddingModel, want.EmbeddingModel))
	}
	if m.Dim != dim {
		reasons = append(reasons, fmt.Sprintf("dim %d -> %d", m.Dim, dim))
	}
	if m.PrefixScheme != want.PrefixScheme {
		reasons = append(reasons, fmt.Sprintf("prefix_scheme %q -> %q", m.PrefixScheme, want.PrefixScheme))
	}
	if m.ChunkerVersion != want.ChunkerVersion {
		reasons = append(reasons, fmt.Sprintf("chunker_version %d -> %d", m.ChunkerVersion, want.ChunkerVersion))
	}
	return &InvalidIndexError{Reasons: reasons}
}

// purge removes every chunk the manifest records under roots.
func (e *Engine) purge(ctx context.Context, m *Manifest, roots []string) (int, int, error) {
	paths := make([]string, 0, len(m.Files))
	for path := range m.Files {
		if underRoots(path, roots) {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)

	var chunks int
	for _, path := range paths {
		ids := m.Files[path].Chunks
		if len(ids) > 0 {
			if err := e.Store.Delete(ctx, nil, nil, ids...); err != nil {
				return 0, 0, fmt.Errorf("purge %s: %w", path, err)
			}
		}
		chunks += removeFileMeta(m, path)
	}
	return len(paths), chunks, nil
}

// reconcile purges the chunks of files that the walk did not see: deleted,
// moved, or newly excluded. Without this, Vektix keeps citing paths that no
// longer exist.
func (e *Engine) reconcile(ctx context.Context, m *Manifest, roots []string, seen map[string]bool) (int, int, error) {
	orphans := make([]string, 0)
	for path := range m.Files {
		if seen[path] || !underRoots(path, roots) {
			continue
		}
		orphans = append(orphans, path)
	}
	sort.Strings(orphans)

	var chunks int
	for _, path := range orphans {
		ids := m.Files[path].Chunks
		if e.DryRun {
			chunks += len(ids)
			continue
		}
		if len(ids) > 0 {
			if err := e.Store.Delete(ctx, nil, nil, ids...); err != nil {
				return len(orphans), chunks, fmt.Errorf("purge orphan %s: %w", path, err)
			}
		}
		chunks += removeFileMeta(m, path)
	}
	return len(orphans), chunks, nil
}

// expireTransientRoots evicts transient roots and purges their chunks according to the LRU retention policy.
func (e *Engine) expireTransientRoots(ctx context.Context, m *Manifest, res *Result) error {
	if len(m.TransientRoots) == 0 {
		return nil
	}

	retentionDays := 7
	if e.IndexCfg != nil && e.IndexCfg.TransientRetentionDays > 0 {
		retentionDays = e.IndexCfg.TransientRetentionDays
	}
	maxRoots := 10
	if e.IndexCfg != nil && e.IndexCfg.MaxTransientRoots > 0 {
		maxRoots = e.IndexCfg.MaxTransientRoots
	}

	now := time.Now().Unix()
	retentionThreshold := now - int64(retentionDays*24*3600)

	type rootAge struct {
		root       string
		lastAccess int64
	}
	var active []rootAge
	var toEvict []string

	for root, lastAccess := range m.TransientRoots {
		if lastAccess < retentionThreshold {
			toEvict = append(toEvict, root)
		} else {
			active = append(active, rootAge{root: root, lastAccess: lastAccess})
		}
	}

	// Sort active roots ascending by lastAccess (oldest first)
	sort.Slice(active, func(i, j int) bool {
		return active[i].lastAccess < active[j].lastAccess
	})

	// If active roots count exceeds maxRoots, evict the oldest active roots
	if len(active) > maxRoots {
		excess := len(active) - maxRoots
		for i := 0; i < excess; i++ {
			toEvict = append(toEvict, active[i].root)
		}
	}

	for _, root := range toEvict {
		var filesToPurge []string
		for path, meta := range m.Files {
			if underRoots(path, []string{root}) || (meta.Transient && strings.HasPrefix(path, root)) {
				filesToPurge = append(filesToPurge, path)
			}
		}
		sort.Strings(filesToPurge)

		for _, path := range filesToPurge {
			ids := m.Files[path].Chunks
			if !e.DryRun && len(ids) > 0 && e.Store != nil {
				if err := e.Store.Delete(ctx, nil, nil, ids...); err != nil {
					return fmt.Errorf("purge transient %s: %w", path, err)
				}
			}
			chunks := removeFileMeta(m, path)
			res.Removed++
			res.RemovedChunks += chunks
		}

		delete(m.TransientRoots, root)

		// Remove from m.Roots as well
		var newRoots []string
		for _, r := range m.Roots {
			if r != root {
				newRoots = append(newRoots, r)
			}
		}
		m.Roots = newRoots
	}

	return nil
}

// ---------------------------------------------------------------------------
// pipeline
// ---------------------------------------------------------------------------

// fileTask carries one file through every stage. Keeping the file (rather than
// the chunk) as the unit of flow is what lets the manifest record a file only
// once all of its chunks are durably stored.
type fileTask struct {
	path      string
	info      os.FileInfo
	hash      string
	oldChunks []string

	content string
	pages   []parser.PDFPage

	chunks  []store.Chunk
	pending int
}

type embedRef struct {
	task *fileTask
	idx  int
}

type pipeline struct {
	e   *Engine
	m   *Manifest
	res *Result

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	fileCh    chan *fileTask
	parsedCh  chan *fileTask
	chunkedCh chan *fileTask
	storeCh   chan *fileTask

	// prev is an immutable snapshot of the manifest taken before the store
	// stage starts mutating it, so the walker can classify files without
	// racing the writer.
	prev map[string]FileMeta
	seen map[string]bool

	mu          sync.Mutex
	quarantined []QuarantineEntry

	errOnce sync.Once
	err     error
}

func newPipeline(e *Engine, m *Manifest, res *Result) *pipeline {
	prev := make(map[string]FileMeta, len(m.Files))
	for k, v := range m.Files {
		prev[k] = v
	}
	return &pipeline{
		e:         e,
		m:         m,
		res:       res,
		fileCh:    make(chan *fileTask, e.ChannelSize),
		parsedCh:  make(chan *fileTask, e.ChannelSize),
		chunkedCh: make(chan *fileTask, e.ChannelSize),
		storeCh:   make(chan *fileTask, e.ChannelSize),
		prev:      prev,
		seen:      make(map[string]bool, len(m.Files)),
	}
}

func (p *pipeline) run(parent context.Context, roots []string) error {
	p.ctx, p.cancel = context.WithCancel(parent)
	defer p.cancel()

	p.wg.Add(1)
	go p.walkStage(roots)

	stage := func(n int, in, out chan *fileTask, work func(*fileTask) error) {
		var workers sync.WaitGroup
		workers.Add(n)
		for i := 0; i < n; i++ {
			go func() {
				defer workers.Done()
				for task := range in {
					if p.ctx.Err() != nil {
						continue // drain so upstream senders are never stuck
					}
					if err := work(task); err != nil {
						p.quarantine(task.path, err)
						continue
					}
					if !p.send(out, task) {
						continue
					}
				}
			}()
		}
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			workers.Wait()
			close(out)
		}()
	}

	stage(p.e.ParseWorkers, p.fileCh, p.parsedCh, p.parseFile)
	stage(p.e.ChunkWorkers, p.parsedCh, p.chunkedCh, p.chunkFile)

	p.wg.Add(1)
	go p.embedStage()

	p.wg.Add(1)
	go p.storeStage()

	p.wg.Wait()

	p.mu.Lock()
	err := p.err
	p.mu.Unlock()
	if err == nil {
		err = parent.Err()
	}
	return err
}

// send delivers to a downstream channel, unblocking if the run is cancelled.
func (p *pipeline) send(ch chan *fileTask, task *fileTask) bool {
	select {
	case ch <- task:
		return true
	case <-p.ctx.Done():
		return false
	}
}

func (p *pipeline) fail(err error) {
	if err == nil {
		return
	}
	p.errOnce.Do(func() {
		p.mu.Lock()
		p.err = err
		p.mu.Unlock()
		p.cancel()
	})
}

func (p *pipeline) quarantine(path string, err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	p.mu.Lock()
	p.quarantined = append(p.quarantined, QuarantineEntry{
		Path:   path,
		Reason: err.Error(),
		Time:   time.Now().UTC(),
	})
	quarantined := len(p.quarantined)
	p.mu.Unlock()
	p.report(func(pr *Progress) { pr.Quarantined = quarantined })
}

func (p *pipeline) snapshotQuarantine() []QuarantineEntry {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]QuarantineEntry, len(p.quarantined))
	copy(out, p.quarantined)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// report updates the shared progress snapshot and notifies the caller.
func (p *pipeline) report(update func(*Progress)) {
	if p.e.OnProgress == nil {
		return
	}
	p.mu.Lock()
	pr := Progress{
		Scanned:     p.res.Scanned,
		Unchanged:   p.res.Unchanged,
		Indexed:     p.res.Indexed,
		Chunks:      p.res.Chunks,
		Quarantined: len(p.quarantined),
	}
	p.mu.Unlock()
	if update != nil {
		update(&pr)
	}
	p.e.OnProgress(pr)
}

// --- walk -------------------------------------------------------------------

func (p *pipeline) walkStage(roots []string) {
	defer p.wg.Done()
	defer close(p.fileCh)

	w := NewWalker(p.e.IndexCfg)
	for _, root := range roots {
		if p.ctx.Err() != nil {
			return
		}
		err := w.Walk(root, p.onFile)
		if err == nil {
			continue
		}
		if p.ctx.Err() != nil {
			return
		}
		// An unreadable root is recorded, not fatal: one unmounted directory
		// must not stop the rest of the run.
		p.quarantine(root, fmt.Errorf("walk: %w", err))
	}
}

func (p *pipeline) onFile(path string, info os.FileInfo) error {
	if err := p.ctx.Err(); err != nil {
		return err
	}

	p.seen[path] = true

	snapshot := &Manifest{Files: p.prev}
	changed, err := snapshot.HasChanged(path, info)
	if err != nil {
		// HashFile failed; HasChanged already reported "changed", so we fall
		// through and let the parse stage produce a real quarantine reason.
		changed = true
	}

	old, known := p.prev[path]

	p.mu.Lock()
	p.res.Scanned++
	switch {
	case !changed:
		p.res.Unchanged++
	case known:
		p.res.Updated++
	default:
		p.res.Added++
	}
	p.mu.Unlock()
	p.report(nil)

	if !changed {
		return nil
	}

	if p.e.DryRun {
		p.mu.Lock()
		p.res.Files = append(p.res.Files, path)
		p.mu.Unlock()
		return nil
	}

	task := &fileTask{path: path, info: info, oldChunks: old.Chunks}
	if !p.send(p.fileCh, task) {
		return p.ctx.Err()
	}
	return nil
}

// --- parse ------------------------------------------------------------------

func (p *pipeline) parseFile(task *fileTask) error {
	hash, err := HashFile(task.path)
	if err != nil {
		return fmt.Errorf("hash: %w", err)
	}
	task.hash = hash

	if strings.EqualFold(filepath.Ext(task.path), ".pdf") {
		ctx, cancel := context.WithTimeout(p.ctx, p.e.PDFTimeout)
		defer cancel()
		doc, err := parser.ParsePDF(ctx, task.path)
		if err != nil {
			// A malformed PDF is quarantined; the run continues.
			return fmt.Errorf("pdf: %w", err)
		}
		task.pages = doc.Pages
		return nil
	}

	f, err := os.Open(task.path)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	doc, err := parser.ParseText(f)
	if err != nil {
		return fmt.Errorf("text: %w", err)
	}
	lines := make([]string, len(doc.Lines))
	for i, l := range doc.Lines {
		lines[i] = l.Content
	}
	task.content = strings.Join(lines, "\n")
	return nil
}

// --- chunk ------------------------------------------------------------------

func (p *pipeline) chunkFile(task *fileTask) error {
	var chunkingCfg config.ChunkingConfig
	if p.e.ChunkingCfg != nil {
		chunkingCfg = *p.e.ChunkingCfg
	} else {
		chunkingCfg = config.DefaultConfig().Chunking
	}

	if task.pages != nil {
		// PDFs are chunked per page so the locator can address a page, which
		// is the only position a PDF reader can actually jump to.
		for _, page := range task.pages {
			for _, c := range chunker.Chunk(task.path, page.Content, chunkingCfg) {
				c.Locator = store.Locator{
					Kind:  store.LocatorPage,
					Start: page.Number,
					End:   page.Number,
				}
				task.chunks = append(task.chunks, c)
			}
		}
	} else {
		task.chunks = chunker.Chunk(task.path, task.content, chunkingCfg)
	}

	for i := range task.chunks {
		task.chunks[i].ID = chunkID(task.path, i)
		task.chunks[i].Path = task.path
		if p.e.Transient {
			task.chunks[i].Transient = true
		}
	}
	task.pending = len(task.chunks)
	task.content = ""
	task.pages = nil
	return nil
}

// chunkID is deterministic, so re-indexing a file overwrites its chunks
// instead of accumulating duplicates, and is filesystem-safe because chromem
// persists one file per document ID.
func chunkID(path string, i int) string {
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:8]) + "-" + strconv.Itoa(i)
}

// --- embed ------------------------------------------------------------------

// embedStage is the bottleneck, so it is the one stage that batches: chunks
// from many files are accumulated into a single /api/embed call, and a file is
// released downstream only once every one of its chunks has a vector.
func (p *pipeline) embedStage() {
	defer p.wg.Done()
	defer close(p.storeCh)

	var (
		refs  []embedRef
		order []*fileTask
	)

	accept := func(task *fileTask) bool {
		if task.pending == 0 {
			// Nothing embeddable (empty or unchunkable file): still record it,
			// otherwise every sync would re-parse it forever.
			return p.send(p.storeCh, task)
		}
		order = append(order, task)
		for i := range task.chunks {
			refs = append(refs, embedRef{task: task, idx: i})
		}
		return true
	}

	// flush embeds the first n refs and releases every file they completed.
	flush := func(n int) bool {
		if n <= 0 {
			return true
		}
		if n > len(refs) {
			n = len(refs)
		}
		batch := refs[:n]

		texts := make([]string, n)
		for i, ref := range batch {
			texts[i] = ref.task.chunks[ref.idx].Content
		}

		// internal/ollama applies the required search_document: prefix.
		resp, err := p.e.Embedder.Embed(p.ctx, ollama.EmbedRequest{
			Model:     p.e.Identity.EmbeddingModel,
			Texts:     texts,
			KeepAlive: p.e.KeepAlive,
		})
		if err != nil {
			p.fail(fmt.Errorf("embed batch of %d: %w", n, err))
			return false
		}
		if len(resp.Embeddings) != n {
			p.fail(fmt.Errorf("embed returned %d vectors for %d texts", len(resp.Embeddings), n))
			return false
		}

		for i, ref := range batch {
			vec := resp.Embeddings[i]
			if len(vec) == 0 {
				p.fail(fmt.Errorf("embed returned an empty vector for %s", ref.task.path))
				return false
			}
			if p.m.Dim == 0 {
				p.m.Dim = len(vec)
			} else if len(vec) != p.m.Dim {
				p.fail(&InvalidIndexError{Reasons: []string{
					fmt.Sprintf("dim %d -> %d", p.m.Dim, len(vec)),
				}})
				return false
			}
			ref.task.chunks[ref.idx].Embedding = vec
			ref.task.pending--
		}
		refs = refs[n:]

		for len(order) > 0 && order[0].pending == 0 {
			task := order[0]
			order = order[1:]
			if !p.send(p.storeCh, task) {
				return false
			}
		}
		return true
	}

	// Accumulate strictly until a full batch is ready or the upstream
	// stages have nothing left to give; a partial batch is only ever sent
	// as the final flush, so a run never pays per-chunk HTTP overhead for
	// chunks that arrive close together.
	for {
		var (
			task *fileTask
			ok   bool
		)
		select {
		case <-p.ctx.Done():
			return
		case task, ok = <-p.chunkedCh:
		}
		if !ok {
			for len(refs) > 0 {
				if !flush(p.e.EmbedBatch) {
					return
				}
			}
			return
		}
		if !accept(task) {
			return
		}
		for len(refs) >= p.e.EmbedBatch {
			if !flush(p.e.EmbedBatch) {
				return
			}
		}
	}
}

// --- store ------------------------------------------------------------------

// storeStage is the only writer of the manifest, so no lock is needed for it,
// and it advances the manifest only after the chunks are durably stored.
func (p *pipeline) storeStage() {
	defer p.wg.Done()

	for task := range p.storeCh {
		if p.ctx.Err() != nil {
			continue
		}

		// Delete before add: an updated file may now produce fewer chunks, and
		// the surplus IDs would otherwise linger as orphans.
		if len(task.oldChunks) > 0 {
			if err := p.e.Store.Delete(p.ctx, nil, nil, task.oldChunks...); err != nil {
				p.fail(fmt.Errorf("replace %s: %w", task.path, err))
				continue
			}
		}
		if len(task.chunks) > 0 {
			if err := p.e.Store.AddDocuments(p.ctx, task.chunks); err != nil {
				p.fail(fmt.Errorf("store %s: %w", task.path, err))
				continue
			}
		}

		ids := make([]string, len(task.chunks))
		for i, c := range task.chunks {
			ids[i] = c.ID
		}
		setFileMeta(p.m, task.path, FileMeta{
			Mtime:     task.info.ModTime().UnixNano(),
			Size:      task.info.Size(),
			Hash:      task.hash,
			Chunks:    ids,
			Transient: p.e.Transient,
		})

		p.mu.Lock()
		p.res.Indexed++
		p.res.Chunks += len(ids)
		p.mu.Unlock()
		p.report(nil)
	}
}

// ---------------------------------------------------------------------------
// manifest maintenance
// ---------------------------------------------------------------------------

// setFileMeta records a file and keeps the dir_counts prefix tree accurate.
func setFileMeta(m *Manifest, path string, meta FileMeta) {
	if m.Files == nil {
		m.Files = make(map[string]FileMeta)
	}
	if old, ok := m.Files[path]; ok {
		addDirCounts(m, path, -len(old.Chunks))
	}
	m.Files[path] = meta
	addDirCounts(m, path, len(meta.Chunks))
}

// removeFileMeta drops a file from the manifest and returns its chunk count.
func removeFileMeta(m *Manifest, path string) int {
	old, ok := m.Files[path]
	if !ok {
		return 0
	}
	addDirCounts(m, path, -len(old.Chunks))
	delete(m.Files, path)
	return len(old.Chunks)
}

// addDirCounts walks from the file's directory to the filesystem root,
// adjusting every ancestor. "" holds the collection total, which is what
// Manifest.ScopeFraction divides by.
func addDirCounts(m *Manifest, path string, delta int) {
	if delta == 0 {
		return
	}
	if m.DirCounts == nil {
		m.DirCounts = make(map[string]int)
	}
	bump := func(key string) {
		m.DirCounts[key] += delta
		if m.DirCounts[key] <= 0 {
			delete(m.DirCounts, key)
		}
	}
	bump("")
	for dir := filepath.Dir(path); ; {
		bump(dir)
		parent := filepath.Dir(dir)
		if parent == dir {
			return
		}
		dir = parent
	}
}

func manifestRoots(m *Manifest) []string {
	roots, err := absolutePaths(m.Roots)
	if err != nil {
		return nil
	}
	return roots
}

func mergeRoots(existing, added []string) []string {
	seen := make(map[string]bool, len(existing)+len(added))
	var out []string
	for _, list := range [][]string{existing, added} {
		for _, r := range list {
			abs, err := config.ExpandPath(r)
			if err != nil {
				abs = r
			}
			if abs, err = filepath.Abs(abs); err != nil {
				continue
			}
			if seen[abs] {
				continue
			}
			seen[abs] = true
			out = append(out, abs)
		}
	}
	sort.Strings(out)
	return out
}

func absolutePaths(paths []string) ([]string, error) {
	var out []string
	for _, path := range paths {
		expanded, err := config.ExpandPath(path)
		if err != nil {
			return nil, err
		}
		abs, err := filepath.Abs(expanded)
		if err != nil {
			return nil, err
		}
		out = append(out, abs)
	}
	return out, nil
}

func underRoots(path string, roots []string) bool {
	for _, root := range roots {
		if path == root || strings.HasPrefix(path, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// reporting
// ---------------------------------------------------------------------------

// Summary renders the run exactly as documented in plan.md.
func (r *Result) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %-10s%6s files\n", "scanned", format.HumanInt(r.Scanned))
	fmt.Fprintf(&b, "  %-10s%6s  (mtime + size match — skipped)\n", "unchanged", format.HumanInt(r.Unchanged))
	fmt.Fprintf(&b, "  %-10s%6s  (re-chunked, re-embedded)\n", "updated", format.HumanInt(r.Updated))
	fmt.Fprintf(&b, "  %-10s%6s\n", "added", format.HumanInt(r.Added))
	fmt.Fprintf(&b, "  %-10s%6s  (%s orphan chunks purged)\n", "removed", format.HumanInt(r.Removed), format.HumanInt(r.RemovedChunks))
	if len(r.Quarantined) > 0 {
		fmt.Fprintf(&b, "  %-10s%6s  (see 'vektix status')\n", "quarantine", format.HumanInt(len(r.Quarantined)))
	}
	fmt.Fprintf(&b, "  %s\n", format.FormatDuration(r.Elapsed))
	return b.String()
}

