package main

// oneshots.go implements the six read-only one-shot subcommands from plan.md's
// action set: locate, read, excerpt, open, copy and list.
//
// Two rules shape everything here:
//
//   - Show, don't summarise. Every byte printed is read back from the file on
//     disk with its real line numbers. Nothing is paraphrased or generated.
//   - Scope is never silent. plan.md: "A silently-scoped tool that can't find a
//     file you know exists is deeply confusing." The active scope is printed on
//     every invocation, and an empty result always names the scope and offers
//     the global retry inline.

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Hendrixx-RE/Vektix/internal/clipboard"
	"github.com/Hendrixx-RE/Vektix/internal/config"
	"github.com/Hendrixx-RE/Vektix/internal/excerpt"
	"github.com/Hendrixx-RE/Vektix/internal/fileops"
	"github.com/Hendrixx-RE/Vektix/internal/format"
	"github.com/Hendrixx-RE/Vektix/internal/index"
	"github.com/Hendrixx-RE/Vektix/internal/ollama"
	"github.com/Hendrixx-RE/Vektix/internal/resolve"
	"github.com/Hendrixx-RE/Vektix/internal/store"
	"github.com/mattn/go-isatty"
)

const (
	// A result found by a single arm at a low rank is dropped from the strong
	// set (plan.md's "minimum-arms rule") and offered as a weak match instead.
	singleArmRankCutoff = 5
	maxWeakMatches      = 3

	excerptLineBudget = 12
	queryCacheSize    = 128
)

// ---------------------------------------------------------------------------
// flags
// ---------------------------------------------------------------------------

type boolFlagState struct {
	set bool
	val bool
}

func (b *boolFlagState) String() string {
	if !b.set {
		return "false"
	}
	return strconv.FormatBool(b.val)
}

func (b *boolFlagState) Set(s string) error {
	v, err := strconv.ParseBool(s)
	if err != nil {
		return err
	}
	b.val = v
	b.set = true
	return nil
}

func (b *boolFlagState) IsBoolFlag() bool {
	return true
}

func isWriterTTY(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
	}
	return false
}

// oneShotFlags holds the flags shared by every one-shot, plus the few
// command-specific ones. `unsafe` is populated here and nowhere else: it is a
// human-supplied CLI flag, never something the router or a document can set.
type oneShotFlags struct {
	scope    string
	global   bool
	jsonOut  bool
	unsafe   bool
	indexNow bool
	limit    int
	lines    string // read
	pathOnly bool   // copy
}

func (f *oneShotFlags) registerCommon(fs *flag.FlagSet) {
	fs.StringVar(&f.scope, "scope", "", "confine the search to this directory")
	fs.BoolVar(&f.global, "global", false, "search the whole index, ignoring the active scope")
	fs.BoolVar(&f.global, "g", false, "shorthand for --global")
	fs.BoolVar(&f.jsonOut, "json", false, "machine-readable output (no colour)")
	fs.BoolVar(&f.unsafe, "unsafe", false, "allow paths outside the indexed roots and the secrets denylist")
	fs.BoolVar(&f.indexNow, "index-now", false, "index unindexed CWD ephemerally before running command")
	fs.IntVar(&f.limit, "limit", 0, "maximum number of results (0 = config default)")
}

// ---------------------------------------------------------------------------
// environment
// ---------------------------------------------------------------------------

// env is everything a handler needs from the outside world. Tests build one
// directly; main builds one from the user's config.
type env struct {
	cfg     *config.Config
	dataDir string
	cwd     string
	stdout  io.Writer
	stderr  io.Writer

	// seams, so tests never touch the real clipboard or launch an editor
	copyFn func(w io.Writer, text string) (string, error)
	openFn func(path string, allowUnsafe bool, cfg *config.Config) error

	corpusOnce *corpus
	corpusErr  error
	corpusDone bool
}

func newEnv() (*env, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	dataDir, err := config.ExpandPath(cfg.General.DataDir)
	if err != nil {
		return nil, fmt.Errorf("resolving data_dir: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolving working directory: %w", err)
	}
	return &env{
		cfg:     &cfg,
		dataDir: dataDir,
		cwd:     cwd,
		stdout:  os.Stdout,
		stderr:  os.Stderr,
		copyFn:  clipboard.CopyTo,
		openFn:  fileops.Open,
	}, nil
}

// dispatch wires a handler into main's subcommand switch.
func dispatch(run func(*env, []string) int, args []string) {
	e, err := newEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "vektix: %v\n", err)
		os.Exit(2)
	}
	if code := run(e, args); code != 0 {
		os.Exit(code)
	}
}

func locateCmd(args []string)  { dispatch(runLocate, args) }
func readCmd(args []string)    { dispatch(runRead, args) }
func excerptCmd(args []string) { dispatch(runExcerpt, args) }
func openCmd(args []string)    { dispatch(runOpen, args) }
func copyCmd(args []string)    { dispatch(runCopy, args) }
func listCmd(args []string)    { dispatch(runList, args) }
func statusCmd(args []string)  { dispatch(runStatus, args) }

// ---------------------------------------------------------------------------
// corpus: the index, loaded once per invocation
// ---------------------------------------------------------------------------

type corpus struct {
	manifest *index.Manifest
	store    *store.Store
	chunks   []store.Chunk

	paths  *resolve.PathIndex
	bm25   *resolve.BM25Index
	vector *resolve.VectorArm

	// chunk IDs listed in the manifest but absent from the store
	missing int

	dbLoadTime time.Duration
}

var errNoIndex = errors.New("no index found")

// loadCorpus opens the manifest and the vector store and rebuilds the two
// model-free arms. No model is loaded and no network call is made here: that is
// what keeps "where is X" on the sub-20ms path.
func (e *env) corpus() (*corpus, error) {
	if e.corpusDone {
		return e.corpusOnce, e.corpusErr
	}
	e.corpusDone = true
	e.corpusOnce, e.corpusErr = e.loadCorpus()
	return e.corpusOnce, e.corpusErr
}

func (e *env) loadCorpus() (*corpus, error) {
	manifestPath := index.ManifestPath(e.dataDir)
	m, err := index.LoadManifest(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w at %s", errNoIndex, format.DisplayPath(manifestPath))
		}
		return nil, fmt.Errorf("reading %s: %w (run 'vektix reindex' to rebuild)", format.DisplayPath(manifestPath), err)
	}
	if err := checkManifest(m, e.cfg); err != nil {
		return nil, err
	}

	dbStart := time.Now()
	db, err := store.NewPersistentDB(index.StorePath(e.dataDir))
	if err != nil {
		return nil, fmt.Errorf("opening the vector store: %w (run 'vektix reindex' to rebuild)", err)
	}
	dbLoadTime := time.Since(dbStart)

	c := &corpus{manifest: m, store: db, dbLoadTime: dbLoadTime}

	// Deterministic order: map iteration order must not change the ranking of
	// two chunks that tie on score.
	files := make([]string, 0, len(m.Files))
	for path := range m.Files {
		files = append(files, path)
	}
	sort.Strings(files)

	var allIDs []string
	idToPath := make(map[string]string)
	for _, path := range files {
		for _, id := range m.Files[path].Chunks {
			allIDs = append(allIDs, id)
			idToPath[id] = path
		}
	}

	chunks, _ := db.GetByIDs(context.Background(), allIDs)
	for i := range chunks {
		if chunks[i].Path == "" {
			chunks[i].Path = idToPath[chunks[i].ID]
		}
	}
	c.chunks = chunks
	c.missing = len(allIDs) - len(chunks)

	c.paths = resolve.NewPathIndex(c.chunks)
	c.bm25 = resolve.NewBM25Index(c.chunks)

	client := ollama.NewClient(ollama.Options{
		Host:              e.cfg.Ollama.Host,
		EmbedTimeout:      time.Duration(e.cfg.Ollama.Timeouts.EmbedBatchSeconds) * time.Second,
		IntentTimeout:     time.Duration(e.cfg.Ollama.Timeouts.IntentSeconds) * time.Second,
		StreamIdleTimeout: time.Duration(e.cfg.Ollama.Timeouts.StreamIdleSeconds) * time.Second,
	})
	c.vector = resolve.NewVectorArm(db, client, ollama.NewEmbeddingCache(queryCacheSize), m,
		e.cfg.Ollama.EmbeddingModel, e.cfg.Ollama.KeepAlive, e.cfg.Search.OversampleFloor)

	return c, nil
}

func (e *env) indexEphemeralSubtree(root string) {
	st, err := store.NewPersistentDB(index.StorePath(e.dataDir))
	if err != nil {
		return
	}
	cli := ollama.NewClient(ollama.Options{
		Host:         e.cfg.Ollama.Host,
		EmbedTimeout: time.Duration(e.cfg.Ollama.Timeouts.EmbedBatchSeconds) * time.Second,
	})
	engine := index.NewEngine(e.cfg, st, cli, e.dataDir)
	engine.Transient = true
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	_, _ = engine.Run(ctx, []string{root}, index.ModeIndex)
}

// checkManifest refuses to search an index that does not match the active
// config rather than returning garbage. Dim, PrefixScheme and ChunkerVersion are
// owned by the indexer and echoed back; the embedding model is the field
// plan.md flags as "changing this requires a reindex".
func checkManifest(m *index.Manifest, cfg *config.Config) error {
	if m.EmbeddingModel == "" || m.Dim == 0 {
		return fmt.Errorf("%w (incomplete header): please run 'vektix reindex'", index.ErrManifestMismatch)
	}
	if err := m.CheckValidity(cfg.Ollama.EmbeddingModel, m.Dim, m.PrefixScheme, m.ChunkerVersion); err != nil {
		return fmt.Errorf("%w (indexed with %q, config says %q)", err, m.EmbeddingModel, cfg.Ollama.EmbeddingModel)
	}
	return nil
}

// indexGuidance turns a corpus load failure into the exact remedy.
func indexGuidance(err error) string {
	if errors.Is(err, errNoIndex) {
		return fmt.Sprintf("%v — run 'vektix index <dir>' to build one, or 'vektix reindex' to rebuild an existing one", err)
	}
	return err.Error()
}

func (c *corpus) countUnder(scope string) int {
	if scope == "" {
		return len(c.chunks)
	}
	n := 0
	for _, ch := range c.chunks {
		if isUnderScope(ch.Path, scope) {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// scope
// ---------------------------------------------------------------------------

type activeScope struct {
	Path       string // "" means global
	Global     bool
	Chunks     int
	Total      int
	HasIndex   bool
	Unindexed  bool // CWD sits outside every indexed root
	IndexError string
}

// resolveActiveScope applies plan.md's scope ladder and attaches the chunk
// counts that make the scope legible.
func (e *env) resolveActiveScope(fl *oneShotFlags) *activeScope {
	sc := &activeScope{}

	roots := make([]string, 0, len(e.cfg.Index.IndexDirs))
	for _, r := range e.cfg.Index.IndexDirs {
		if exp, err := config.ExpandPath(r); err == nil {
			roots = append(roots, exp)
		}
	}

	res, err := resolve.ResolveScope(e.cwd, fl.scope, fl.global, e.cfg.General.ScopeMode, roots)
	if err != nil {
		sc.Global = true
	} else if res.RequiresPrompt && fl.scope == "" {
		if fl.indexNow {
			if !fl.jsonOut {
				fmt.Fprintf(e.stderr, "indexing %s ephemerally (transient)...\n", displayPath(e.cwd))
			}
			e.indexEphemeralSubtree(e.cwd)
			e.corpusDone = false
			e.corpusOnce = nil
			e.corpusErr = nil
			sc.Path = e.cwd
			sc.Global = false
			sc.Unindexed = false
		} else {
			// A one-shot cannot prompt. Falling back to global is the honest choice:
			// scoping to an unindexed directory would silently return nothing.
			sc.Global = true
			sc.Unindexed = true
			sc.Path = ""
		}
	} else {
		sc.Path = res.Path
		sc.Global = res.Path == ""
	}

	c, cerr := e.corpus()
	if cerr != nil {
		sc.IndexError = indexGuidance(cerr)
		return sc
	}
	sc.HasIndex = true
	sc.Total = len(c.chunks)
	sc.Chunks = c.countUnder(sc.Path)
	return sc
}

// name is how the scope is referred to in prose: "global" or a ~-shortened path.
func (sc *activeScope) name() string {
	if sc.Global || sc.Path == "" {
		return "global"
	}
	return format.DisplayPath(sc.Path)
}

// describe is the scope plus its chunk count, e.g.
// "~/projects/go/vektix (412 chunks)" — the phrase reused in empty results.
func (sc *activeScope) describe() string {
	if !sc.HasIndex {
		return fmt.Sprintf("%s (no index)", sc.name())
	}
	return fmt.Sprintf("%s (%s chunks)", sc.name(), format.HumanInt(sc.Chunks))
}

func (sc *activeScope) banner() string {
	switch {
	case !sc.HasIndex:
		return fmt.Sprintf("scope: %s — %s", sc.name(), sc.IndexError)
	case sc.Global:
		return fmt.Sprintf("scope: global (%s chunks)", format.HumanInt(sc.Total))
	default:
		return fmt.Sprintf("scope: %s (%s of %s chunks) — --global searches everything",
			sc.name(), format.HumanInt(sc.Chunks), format.HumanInt(sc.Total))
	}
}

func (sc *activeScope) payload() map[string]any {
	p := map[string]any{
		"path":   sc.Path,
		"name":   sc.name(),
		"global": sc.Global || sc.Path == "",
		"chunks": sc.Chunks,
		"total":  sc.Total,
	}
	if sc.Global {
		p["chunks"] = sc.Total
	}
	if sc.Unindexed {
		p["cwd_unindexed"] = true
	}
	if sc.IndexError != "" {
		p["index_error"] = sc.IndexError
	}
	return p
}

// printScope makes the active scope legible on every single invocation. It goes
// to stderr so stdout stays pipe-clean; in --json mode the scope travels inside
// the payload instead.
func (e *env) printScope(fl *oneShotFlags, sc *activeScope) {
	if fl.jsonOut {
		return
	}
	fmt.Fprintln(e.stderr, sc.banner())
	if sc.Unindexed {
		fmt.Fprintf(e.stderr, "note: %s is not under any indexed root — searching globally. Pass --index-now to index ephemerally, or run 'vektix index %s'.\n",
			format.DisplayPath(e.cwd), format.DisplayPath(e.cwd))
	}
}

// emptyMessage never lets the output imply a file does not exist when it is
// merely out of scope.
func (e *env) emptyMessage(sc *activeScope, query string, globalHits int) string {
	if sc.Global {
		return fmt.Sprintf("no matches for %q in scope %s; nothing else is indexed — run 'vektix index <dir>' to add it, or 'vektix sync' if it has moved",
			query, sc.describe())
	}
	if globalHits > 0 {
		return fmt.Sprintf("no matches for %q in scope %s; %d match outside this scope — retry with --global",
			query, sc.describe(), globalHits)
	}
	return fmt.Sprintf("no matches for %q in scope %s; nothing in the full index either (%s chunks) — retry with --global to confirm, or 'vektix index <dir>' if it was never indexed",
		query, sc.describe(), format.HumanInt(sc.Total))
}

// ---------------------------------------------------------------------------
// search
// ---------------------------------------------------------------------------

type searchResult struct {
	chunk    store.Chunk
	score    float64
	arms     []string
	bestRank int
	rank     int
}

func (r searchResult) armLabel() string {
	return fmt.Sprintf("(%s, rank %d)", strings.Join(r.arms, "+"), r.rank)
}

// search runs the arms and fuses them. useVector is a request, not a promise:
// the model-free arms run first and the embedder is only consulted when they
// fail to produce a strong answer, which is what keeps the hot path model-free.
func (c *corpus) search(ctx context.Context, e *env, query, scope string, k int, useVector bool) (strong, weak []searchResult, warnings []string) {
	if query == "" {
		return nil, nil, nil
	}

	if scope != "" && c.manifest != nil {
		if c.manifest.TouchPath(scope) {
			_ = c.manifest.SaveManifest(index.ManifestPath(e.dataDir))
		}
	}

	lists := []resolve.ResultList{
		c.paths.Search(query, scope),
		c.bm25.Search(query, scope),
	}
	labels := []string{"path", "bm25"}

	fuse := func() ([]searchResult, []searchResult) {
		return classify(lists, labels, e.cfg, scope, k)
	}

	strong, weak = fuse()
	if !useVector && len(strong) > 0 {
		return strong, weak, nil
	}

	vecK := k * 4
	if vecK < 20 {
		vecK = 20
	}
	vres, err := c.vector.Search(ctx, query, scope, vecK)
	if err != nil {
		warnings = append(warnings, ollamaGuidance(e.cfg.Ollama.Host, err))
		return strong, weak, warnings
	}
	lists = append(lists, vres)
	labels = append(labels, "vec")

	strong, weak = fuse()
	return strong, weak, warnings
}

// classify fuses the arms with RRF and splits the ranking at plan.md's cutoff:
// a document seen by a single arm at a low rank is a weak match, not a hit.
func classify(lists []resolve.ResultList, labels []string, cfg *config.Config, scope string, k int) (strong, weak []searchResult) {
	type armHit struct {
		arm  string
		rank int
	}
	hits := map[string][]armHit{}
	for i, list := range lists {
		for r, ch := range list {
			hits[ch.ID] = append(hits[ch.ID], armHit{labels[i], r + 1})
		}
	}

	rrfK := cfg.Search.RRFK
	if rrfK <= 0 {
		rrfK = 60
	}
	minArms := cfg.Search.MinArms
	if minArms < 1 {
		minArms = 1
	}

	fused := resolve.Fuse(lists, rrfK, 1, 500)
	for _, sc := range fused {
		// Guard against prefix bleed: the arms filter on a raw string prefix, so
		// scope /p/app would also admit /p/appliance.md.
		if !isUnderScope(sc.Path, scope) {
			continue
		}

		res := searchResult{chunk: sc.Chunk, score: sc.Score, bestRank: 1 << 30}
		seen := map[string]bool{}
		for _, h := range hits[sc.ID] {
			if !seen[h.arm] {
				seen[h.arm] = true
				res.arms = append(res.arms, h.arm)
			}
			if h.rank < res.bestRank {
				res.bestRank = h.rank
			}
		}

		isStrong := len(res.arms) >= minArms && (len(res.arms) >= 2 || res.bestRank <= singleArmRankCutoff)
		if isStrong {
			strong = append(strong, res)
		} else {
			weak = append(weak, res)
		}
	}

	if len(strong) > k {
		strong = strong[:k]
	}
	if len(weak) > maxWeakMatches {
		weak = weak[:maxWeakMatches]
	}
	for i := range strong {
		strong[i].rank = i + 1
	}
	for i := range weak {
		weak[i].rank = i + 1
	}
	return strong, weak
}

// dedupeByPath collapses a chunk ranking into a path ranking, which is what
// locate returns.
func dedupeByPath(results []searchResult) []searchResult {
	seen := map[string]bool{}
	var out []searchResult
	for _, r := range results {
		if seen[r.chunk.Path] {
			continue
		}
		seen[r.chunk.Path] = true
		r.rank = len(out) + 1
		out = append(out, r)
	}
	return out
}

// globalHitCount re-runs only the model-free arms without a scope, so an empty
// scoped result can honestly say whether the file exists elsewhere.
func (c *corpus) globalHitCount(e *env, query string, k int) int {
	lists := []resolve.ResultList{
		c.paths.Search(query, ""),
		c.bm25.Search(query, ""),
	}
	strong, _ := classify(lists, []string{"path", "bm25"}, e.cfg, "", k)
	return len(dedupeByPath(strong))
}

func ollamaGuidance(host string, err error) string {
	return fmt.Sprintf("semantic (vector) search unavailable: Ollama is not answering at %s (%v). "+
		"Install it from https://ollama.com and start it with 'ollama serve', then 'vektix setup' to pull the models. "+
		"Continuing with path + keyword matching only.", host, err)
}

// ---------------------------------------------------------------------------
// locate
// ---------------------------------------------------------------------------

func runLocate(e *env, args []string) int {
	fl := &oneShotFlags{}
	fs := flag.NewFlagSet("locate", flag.ContinueOnError)
	fs.SetOutput(e.stderr)
	fl.registerCommon(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	query := strings.Join(fs.Args(), " ")
	if query == "" {
		fmt.Fprintln(e.stderr, "usage: vektix locate [--scope <dir>] [--global] [--json] <query>")
		return 2
	}

	sc := e.resolveActiveScope(fl)
	e.printScope(fl, sc)
	if !sc.HasIndex {
		return e.fail(fl, "locate", sc, sc.IndexError, 2)
	}
	c, _ := e.corpus()

	ctx := context.Background()
	k := e.limit(fl)
	// locate is the model-free hot path: the embedder is only consulted if the
	// path and keyword arms come back empty-handed.
	strong, weak, warnings := c.search(ctx, e, query, sc.Path, k, false)
	strong = dedupeByPath(strong)
	weak = dedupeByPath(weak)

	if len(strong) == 0 {
		return e.reportEmpty(fl, "locate", sc, c, query, weak, warnings, k)
	}

	if fl.jsonOut {
		return e.emitJSON(map[string]any{
			"command":  "locate",
			"query":    query,
			"scope":    sc.payload(),
			"results":  locatePayload(strong),
			"warnings": warnings,
		})
	}

	e.printWarnings(warnings)
	for _, r := range strong {
		fmt.Fprintf(e.stdout, "%2d. %s  %s\n", r.rank, format.DisplayPath(r.chunk.Path), r.armLabel())
	}
	if len(weak) > 0 {
		fmt.Fprintf(e.stderr, "\nweaker matches below the cutoff in scope %s:\n", sc.describe())
		for _, r := range weak {
			fmt.Fprintf(e.stderr, "    %s  %s\n", format.DisplayPath(r.chunk.Path), r.armLabel())
		}
	}
	return 0
}

func locatePayload(results []searchResult) []map[string]any {
	out := make([]map[string]any, 0, len(results))
	for _, r := range results {
		out = append(out, map[string]any{
			"rank":  r.rank,
			"path":  r.chunk.Path,
			"score": r.score,
			"arms":  r.arms,
		})
	}
	return out
}

// reportEmpty is the single place an empty result is produced, so the scope name
// and the global retry can never be omitted by accident.
func (e *env) reportEmpty(fl *oneShotFlags, cmd string, sc *activeScope, c *corpus, query string, weak []searchResult, warnings []string, k int) int {
	globalHits := 0
	if !sc.Global {
		globalHits = c.globalHitCount(e, query, k)
	}
	msg := e.emptyMessage(sc, query, globalHits)

	if fl.jsonOut {
		e.emitJSON(map[string]any{
			"command":      cmd,
			"query":        query,
			"scope":        sc.payload(),
			"results":      []map[string]any{},
			"weak_matches": locatePayload(weak),
			"message":      msg,
			"retry_global": !sc.Global,
			"warnings":     warnings,
		})
		return 1
	}

	e.printWarnings(warnings)
	fmt.Fprintln(e.stderr, msg)
	if len(weak) > 0 {
		fmt.Fprintln(e.stderr, "nearest weak matches (below the cutoff, shown rather than guessed):")
		for _, r := range weak {
			fmt.Fprintf(e.stderr, "    %s  %s\n", format.DisplayPath(r.chunk.Path), r.armLabel())
		}
	}
	return 1
}

func (e *env) printWarnings(warnings []string) {
	for _, w := range warnings {
		fmt.Fprintf(e.stderr, "warning: %s\n", w)
	}
}

// ---------------------------------------------------------------------------
// read
// ---------------------------------------------------------------------------

func runRead(e *env, args []string) int {
	fl := &oneShotFlags{}
	fs := flag.NewFlagSet("read", flag.ContinueOnError)
	fs.SetOutput(e.stderr)
	fl.registerCommon(fs)
	fs.StringVar(&fl.lines, "lines", "", "line range to print, e.g. 41-47, 41- or -20")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	arg := strings.Join(fs.Args(), " ")
	if arg == "" {
		fmt.Fprintln(e.stderr, "usage: vektix read [--lines A-B] [--json] <path|path:A-B|query>")
		return 2
	}

	sc := e.resolveActiveScope(fl)
	e.printScope(fl, sc)

	tgt, code := e.resolveTarget(fl, sc, "read", arg, false)
	if tgt == nil {
		return code
	}

	start, end := tgt.start, tgt.end
	if fl.lines != "" {
		s, en, err := parseLineRange(fl.lines)
		if err != nil {
			return e.fail(fl, "read", sc, err.Error(), 2)
		}
		start, end = s, en
	}

	allowUnsafe := fl.unsafe
	data, err := fileops.ReadFile(tgt.path, allowUnsafe, e.cfg)
	if err != nil {
		return e.fail(fl, "read", sc, readErrorMessage(tgt.path, err), 2)
	}

	content := string(data)
	trailingNewline := strings.HasSuffix(content, "\n")
	lines := strings.Split(content, "\n")
	if trailingNewline {
		lines = lines[:len(lines)-1]
	}

	if start == 0 && end == 0 {
		start, end = 1, len(lines)
	} else {
		if start < 1 {
			start = 1
		}
		if end == 0 || end > len(lines) {
			end = len(lines)
		}
		if start > len(lines) {
			return e.fail(fl, "read", sc,
				fmt.Sprintf("%s has %d lines; line %d is past the end", format.DisplayPath(tgt.path), len(lines), start), 1)
		}
		if start > end {
			start = end
		}
	}

	body := strings.Join(lines[start-1:end], "\n")

	if fl.jsonOut {
		payload := map[string]any{
			"command":    "read",
			"path":       tgt.path,
			"start_line": start,
			"end_line":   end,
			"lines":      end - start + 1,
			"content":    body,
			"scope":      sc.payload(),
		}
		if tgt.query != "" {
			payload["resolved_from_query"] = tgt.query
		}
		return e.emitJSON(payload)
	}

	// Header on stderr, exact bytes on stdout: `vektix read x | wc -l` stays true.
	fmt.Fprintf(e.stderr, "%s:%d-%d (%d lines)\n", format.DisplayPath(tgt.path), start, end, end-start+1)
	fmt.Fprintln(e.stdout, body)
	return 0
}

func readErrorMessage(path string, err error) string {
	msg := err.Error()
	if strings.Contains(msg, "secrets denylist") {
		return fmt.Sprintf("refused to read %s: it matches the secrets denylist. Pass --unsafe if you really mean it.", format.DisplayPath(path))
	}
	if strings.Contains(msg, "outside indexed roots") {
		return fmt.Sprintf("refused to read %s: it is outside the indexed roots and the current directory. Pass --unsafe to override.", format.DisplayPath(path))
	}
	if os.IsNotExist(err) {
		return fmt.Sprintf("%s is no longer on disk — the index may be stale, run 'vektix sync'", format.DisplayPath(path))
	}
	return fmt.Sprintf("cannot read %s: %v", format.DisplayPath(path), err)
}

// ---------------------------------------------------------------------------
// excerpt
// ---------------------------------------------------------------------------

func runExcerpt(e *env, args []string) int {
	fl := &oneShotFlags{}
	fs := flag.NewFlagSet("excerpt", flag.ContinueOnError)
	fs.SetOutput(e.stderr)
	fl.registerCommon(fs)
	var noColorFlag boolFlagState
	fs.Var(&noColorFlag, "no-color", "suppress ANSI colour highlight (defaults to true when stdout is not a TTY)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	query := strings.Join(fs.Args(), " ")
	if query == "" {
		fmt.Fprintln(e.stderr, "usage: vektix excerpt [--scope <dir>] [--global] [--json] [--no-color] <query>")
		return 2
	}

	noColor := !isWriterTTY(e.stdout)
	if noColorFlag.set {
		noColor = noColorFlag.val
	}

	sc := e.resolveActiveScope(fl)
	e.printScope(fl, sc)
	if !sc.HasIndex {
		return e.fail(fl, "excerpt", sc, sc.IndexError, 2)
	}
	c, _ := e.corpus()

	k := e.limit(fl)
	// excerpt is a passage query: semantics matter, so the vector arm is asked
	// for. If Ollama is down the arm degrades to a warning and lexical results.
	strong, weak, warnings := c.search(context.Background(), e, query, sc.Path, k, true)
	if len(strong) == 0 {
		return e.reportEmpty(fl, "excerpt", sc, c, query, weak, warnings, k)
	}

	var payloads []map[string]any
	var rendered []string
	var skipped []string

	for _, r := range strong {
		text, loc, err := e.excerptText(r.chunk, fl.unsafe)
		if err != nil {
			skipped = append(skipped, readErrorMessage(r.chunk.Path, err))
			continue
		}
		if fl.jsonOut {
			payloads = append(payloads, map[string]any{
				"rank":       r.rank,
				"path":       r.chunk.Path,
				"start_line": loc.Start,
				"end_line":   loc.End,
				"kind":       string(loc.Kind),
				"symbol":     loc.Symbol,
				"score":      r.score,
				"arms":       r.arms,
				"text":       text,
			})
			continue
		}
		rendered = append(rendered, excerpt.Render(r.chunk, text, loc, excerpt.RenderConfig{
			HeaderRankInfo: r.armLabel(),
			NoColor:        noColor,
		}))
	}

	if fl.jsonOut {
		if payloads == nil {
			payloads = []map[string]any{}
		}
		return e.emitJSON(map[string]any{
			"command":  "excerpt",
			"query":    query,
			"scope":    sc.payload(),
			"results":  payloads,
			"skipped":  skipped,
			"warnings": warnings,
		})
	}

	e.printWarnings(warnings)
	for _, s := range skipped {
		fmt.Fprintf(e.stderr, "skipped: %s\n", s)
	}
	if len(rendered) == 0 {
		fmt.Fprintf(e.stderr, "every match in scope %s was unreadable (see above)\n", sc.describe())
		return 1
	}
	for i, block := range rendered {
		if i > 0 {
			fmt.Fprintln(e.stdout)
		}
		fmt.Fprint(e.stdout, block)
	}
	top := strong[0]
	fmt.Fprintf(e.stderr, "\n  open: vektix open %s   copy: vektix copy %s\n",
		format.DisplayPath(top.chunk.Path), format.DisplayPath(top.chunk.Path))
	return 0
}

// excerptText returns the real bytes around a chunk, expanded to a natural
// boundary. Nothing is paraphrased: on any failure the caller reports the
// failure rather than substituting text.
func (e *env) excerptText(chunk store.Chunk, allowUnsafe bool) (string, store.Locator, error) {
	if chunk.Locator.Kind == store.LocatorPage {
		// A PDF's bytes on disk are not its text; the chunk already holds the
		// extracted page text, so it is shown as-is with its page locator.
		return chunk.Content, chunk.Locator, nil
	}
	source, err := fileops.ReadFile(chunk.Path, allowUnsafe, e.cfg)
	if err != nil {
		return "", store.Locator{}, err
	}
	text, loc := excerpt.Expand(chunk, source, excerpt.ExpandConfig{MaxLines: excerptLineBudget})
	return text, loc, nil
}

// ---------------------------------------------------------------------------
// open
// ---------------------------------------------------------------------------

func runOpen(e *env, args []string) int {
	fl := &oneShotFlags{}
	fs := flag.NewFlagSet("open", flag.ContinueOnError)
	fs.SetOutput(e.stderr)
	fl.registerCommon(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	arg := strings.Join(fs.Args(), " ")
	if arg == "" {
		fmt.Fprintln(e.stderr, "usage: vektix open [--scope <dir>] [--global] [--json] <path|query>")
		return 2
	}

	sc := e.resolveActiveScope(fl)
	e.printScope(fl, sc)

	tgt, code := e.resolveTarget(fl, sc, "open", arg, false)
	if tgt == nil {
		return code
	}

	if err := e.openFn(tgt.path, fl.unsafe, e.cfg); err != nil {
		return e.fail(fl, "open", sc, fmt.Sprintf("could not open %s: %v", format.DisplayPath(tgt.path), err), 1)
	}

	if fl.jsonOut {
		payload := map[string]any{"command": "open", "path": tgt.path, "scope": sc.payload()}
		if tgt.query != "" {
			payload["resolved_from_query"] = tgt.query
		}
		return e.emitJSON(payload)
	}
	fmt.Fprintf(e.stderr, "opened %s\n", format.DisplayPath(tgt.path))
	return 0
}

// ---------------------------------------------------------------------------
// copy
// ---------------------------------------------------------------------------

func runCopy(e *env, args []string) int {
	fl := &oneShotFlags{}
	fs := flag.NewFlagSet("copy", flag.ContinueOnError)
	fs.SetOutput(e.stderr)
	fl.registerCommon(fs)
	fs.BoolVar(&fl.pathOnly, "path", false, "copy the path instead of the excerpt")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	rest := fs.Args()
	// "vektix copy path <query>" — plan.md's "copy the path" phrasing.
	if len(rest) > 1 && rest[0] == "path" {
		fl.pathOnly = true
		rest = rest[1:]
	}
	arg := strings.Join(rest, " ")
	if arg == "" {
		fmt.Fprintln(e.stderr, "usage: vektix copy [path] [--json] <path|path:A-B|query>")
		return 2
	}

	sc := e.resolveActiveScope(fl)
	e.printScope(fl, sc)

	// A path copy never needs the file's content, so it never needs the embedder.
	tgt, code := e.resolveTarget(fl, sc, "copy", arg, !fl.pathOnly)
	if tgt == nil {
		return code
	}

	mode := "excerpt"
	payload := format.DisplayPath(tgt.path)
	start, end := tgt.start, tgt.end

	if fl.pathOnly {
		mode = "path"
	} else {
		text, loc, err := e.copyBody(tgt, fl)
		if err != nil {
			return e.fail(fl, "copy", sc, readErrorMessage(tgt.path, err), 2)
		}
		payload = text
		start, end = loc.Start, loc.End
	}

	// The OSC 52 fallback writes an escape sequence; in --json mode it must go to
	// the terminal on stderr, not into the payload on stdout.
	sink := e.stdout
	if fl.jsonOut {
		sink = e.stderr
	}
	mechanism, err := e.copyFn(sink, payload)
	if err != nil {
		return e.fail(fl, "copy", sc, fmt.Sprintf("clipboard copy failed: %v", err), 1)
	}

	if fl.jsonOut {
		out := map[string]any{
			"command":   "copy",
			"mode":      mode,
			"path":      tgt.path,
			"content":   payload,
			"mechanism": mechanism,
			"scope":     sc.payload(),
		}
		if mode == "excerpt" {
			out["start_line"] = start
			out["end_line"] = end
		}
		if tgt.query != "" {
			out["resolved_from_query"] = tgt.query
		}
		return e.emitJSON(out)
	}

	if mode == "path" {
		fmt.Fprintf(e.stderr, "copied path %s (%s)\n", format.DisplayPath(tgt.path), mechanism)
	} else {
		fmt.Fprintf(e.stderr, "copied %s:%d-%d (%s)\n", format.DisplayPath(tgt.path), start, end, mechanism)
	}
	return 0
}

// copyBody returns the exact bytes to place on the clipboard: no gutters, no
// colour codes, no summary.
func (e *env) copyBody(tgt *target, fl *oneShotFlags) (string, store.Locator, error) {
	allowUnsafe := fl.unsafe
	if tgt.chunk != nil && tgt.start == 0 {
		text, loc, err := e.excerptText(*tgt.chunk, allowUnsafe)
		return text, loc, err
	}

	data, err := fileops.ReadFile(tgt.path, allowUnsafe, e.cfg)
	if err != nil {
		return "", store.Locator{}, err
	}
	content := string(data)
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")

	start, end := tgt.start, tgt.end
	if start == 0 {
		start, end = 1, len(lines)
	}
	if start < 1 {
		start = 1
	}
	if end == 0 || end > len(lines) {
		end = len(lines)
	}
	if start > len(lines) {
		return "", store.Locator{}, fmt.Errorf("line %d is past the end of the file (%d lines)", start, len(lines))
	}
	return strings.Join(lines[start-1:end], "\n"),
		store.Locator{Kind: store.LocatorLineRange, Start: start, End: end}, nil
}

// ---------------------------------------------------------------------------
// list
// ---------------------------------------------------------------------------

func runList(e *env, args []string) int {
	fl := &oneShotFlags{}
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(e.stderr)
	fl.registerCommon(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	sc := e.resolveActiveScope(fl)
	e.printScope(fl, sc)

	dir := strings.Join(fs.Args(), " ")
	if dir == "" {
		dir = sc.Path
		if dir == "" {
			dir = e.cwd
		}
	}

	allowUnsafe := fl.unsafe
	safeDir, err := fileops.ResolvePath(dir, allowUnsafe, e.cfg)
	if err != nil {
		return e.fail(fl, "list", sc, readErrorMessage(dir, err), 2)
	}
	entries, err := os.ReadDir(safeDir)
	if err != nil {
		return e.fail(fl, "list", sc, readErrorMessage(safeDir, err), 2)
	}

	chunkCounts := map[string]int{}
	if c, cerr := e.corpus(); cerr == nil {
		for path, meta := range c.manifest.Files {
			chunkCounts[path] = len(meta.Chunks)
		}
	}

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return entries[i].Name() < entries[j].Name()
	})

	type row struct {
		Name    string `json:"name"`
		Path    string `json:"path"`
		IsDir   bool   `json:"is_dir"`
		Size    int64  `json:"size"`
		Indexed int    `json:"indexed_chunks"`
	}
	rows := make([]row, 0, len(entries))
	for _, ent := range entries {
		full := filepath.Join(safeDir, ent.Name())
		r := row{Name: ent.Name(), Path: full, IsDir: ent.IsDir(), Indexed: chunkCounts[full]}
		if info, err := ent.Info(); err == nil && !ent.IsDir() {
			r.Size = info.Size()
		}
		rows = append(rows, r)
	}

	outsideScope := sc.Path != "" && !isUnderScope(safeDir, sc.Path)

	if fl.jsonOut {
		return e.emitJSON(map[string]any{
			"command":        "list",
			"path":           safeDir,
			"entries":        rows,
			"scope":          sc.payload(),
			"outside_scope":  outsideScope,
			"entry_count":    len(rows),
			"scope_reminder": sc.describe(),
		})
	}

	fmt.Fprintf(e.stderr, "listing: %s", format.DisplayPath(safeDir))
	if outsideScope {
		fmt.Fprintf(e.stderr, " (outside the active scope %s)", sc.name())
	}
	fmt.Fprintln(e.stderr)

	if len(rows) == 0 {
		fmt.Fprintf(e.stderr, "%s is empty\n", format.DisplayPath(safeDir))
		return 1
	}
	for _, r := range rows {
		name := r.Name
		if r.IsDir {
			name += "/"
		}
		switch {
		case r.IsDir:
			fmt.Fprintf(e.stdout, "%-40s\n", name)
		case r.Indexed > 0:
			fmt.Fprintf(e.stdout, "%-40s %8s  indexed: %d chunks\n", name, format.HumanBytes(r.Size), r.Indexed)
		default:
			fmt.Fprintf(e.stdout, "%-40s %8s  not indexed\n", name, format.HumanBytes(r.Size))
		}
	}
	return 0
}

// ---------------------------------------------------------------------------
// target resolution
// ---------------------------------------------------------------------------

// target is a concrete file, either named directly on the command line or found
// by the same ranked search locate uses.
type target struct {
	path       string
	start, end int
	query      string
	chunk      *store.Chunk
}

// resolveTarget accepts a path, a path:A-B reference (the form excerpt prints),
// or a natural-language query. Every branch ends in fileops.ResolvePath, so
// confinement and the secrets denylist apply no matter how the path was reached.
func (e *env) resolveTarget(fl *oneShotFlags, sc *activeScope, cmd, arg string, useVector bool) (*target, int) {
	allowUnsafe := fl.unsafe
	literal, start, end := splitPathRange(arg)

	if p, err := fileops.ResolvePath(literal, allowUnsafe, e.cfg); err == nil {
		if info, serr := os.Stat(p); serr == nil && !info.IsDir() {
			return &target{path: p, start: start, end: end}, 0
		}
	} else if looksLikePath(literal) {
		// A real path that confinement or the denylist refused: say so plainly
		// rather than silently falling through to a fuzzy search.
		if _, serr := os.Lstat(literal); serr == nil {
			return nil, e.fail(fl, cmd, sc, readErrorMessage(literal, err), 2)
		}
	}

	if !sc.HasIndex {
		return nil, e.fail(fl, cmd, sc,
			fmt.Sprintf("%s is not a file and there is no index to search: %s", arg, sc.IndexError), 2)
	}
	c, _ := e.corpus()

	k := e.limit(fl)
	strong, weak, warnings := c.search(context.Background(), e, arg, sc.Path, k, useVector)
	if len(strong) == 0 {
		return nil, e.reportEmpty(fl, cmd, sc, c, arg, dedupeByPath(weak), warnings, k)
	}

	e.printWarnings(warnings)
	top := strong[0]
	safe, err := fileops.ResolvePath(top.chunk.Path, allowUnsafe, e.cfg)
	if err != nil {
		return nil, e.fail(fl, cmd, sc, readErrorMessage(top.chunk.Path, err), 2)
	}

	if !fl.jsonOut {
		fmt.Fprintf(e.stderr, "resolved %q → %s %s\n", arg, format.DisplayPath(safe), top.armLabel())
		others := dedupeByPath(strong)
		if len(others) > 1 {
			fmt.Fprint(e.stderr, "other candidates in scope: ")
			var names []string
			for _, o := range others[1:] {
				names = append(names, format.DisplayPath(o.chunk.Path))
			}
			fmt.Fprintln(e.stderr, strings.Join(names, ", "))
		}
	}

	chunk := top.chunk
	return &target{path: safe, start: start, end: end, query: arg, chunk: &chunk}, 0
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func (e *env) limit(fl *oneShotFlags) int {
	if fl.limit > 0 {
		return fl.limit
	}
	if e.cfg.Search.MaxResults > 0 {
		return e.cfg.Search.MaxResults
	}
	return 8
}

func (e *env) emitJSON(payload map[string]any) int {
	enc := json.NewEncoder(e.stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		fmt.Fprintf(e.stderr, "vektix: encoding json: %v\n", err)
		return 2
	}
	return 0
}

// fail reports an error the same way in both output modes.
func (e *env) fail(fl *oneShotFlags, cmd string, sc *activeScope, msg string, code int) int {
	if fl.jsonOut {
		payload := map[string]any{"command": cmd, "ok": false, "message": msg}
		if sc != nil {
			payload["scope"] = sc.payload()
		}
		e.emitJSON(payload)
		return code
	}
	fmt.Fprintf(e.stderr, "vektix: %s\n", msg)
	return code
}

// isUnderScope is a path-component-aware prefix test: /p/app must not match
// /p/appliance.md.
func isUnderScope(path, scope string) bool {
	if scope == "" {
		return true
	}
	if path == scope {
		return true
	}
	sep := string(filepath.Separator)
	return strings.HasPrefix(path, strings.TrimSuffix(scope, sep)+sep)
}

// splitPathRange understands the `path:41-47` reference excerpt prints.
func splitPathRange(arg string) (path string, start, end int) {
	idx := strings.LastIndex(arg, ":")
	if idx <= 0 {
		return arg, 0, 0
	}
	s, e, err := parseLineRange(arg[idx+1:])
	if err != nil {
		return arg, 0, 0
	}
	return arg[:idx], s, e
}

// parseLineRange accepts "41-47", "41", "41-" and "-47".
func parseLineRange(spec string) (int, int, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return 0, 0, fmt.Errorf("empty line range")
	}
	if !strings.Contains(spec, "-") {
		n, err := strconv.Atoi(spec)
		if err != nil || n < 1 {
			return 0, 0, fmt.Errorf("invalid line range %q: expected forms are 41-47, 41, 41- or -47", spec)
		}
		return n, n, nil
	}
	parts := strings.SplitN(spec, "-", 2)
	var start, end int
	var err error
	if parts[0] != "" {
		if start, err = strconv.Atoi(parts[0]); err != nil || start < 1 {
			return 0, 0, fmt.Errorf("invalid line range %q: expected forms are 41-47, 41, 41- or -47", spec)
		}
	} else {
		start = 1
	}
	if parts[1] != "" {
		if end, err = strconv.Atoi(parts[1]); err != nil || end < 1 {
			return 0, 0, fmt.Errorf("invalid line range %q: expected forms are 41-47, 41, 41- or -47", spec)
		}
	}
	if end != 0 && end < start {
		return 0, 0, fmt.Errorf("invalid line range %q: end is before start", spec)
	}
	return start, end, nil
}

func looksLikePath(arg string) bool {
	return strings.ContainsAny(arg, "/~") || strings.HasPrefix(arg, ".") || filepath.Ext(arg) != ""
}

// ---------------------------------------------------------------------------
// status
// ---------------------------------------------------------------------------

func runStatus(e *env, args []string) int {
	fl := &oneShotFlags{}
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(e.stderr)
	fl.registerCommon(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	sc := e.resolveActiveScope(fl)

	manifestPath := index.ManifestPath(e.dataDir)
	manifestInfo, manifestStatErr := os.Stat(manifestPath)
	hasManifest := manifestStatErr == nil

	quarantine, qErr := index.LoadQuarantine(index.QuarantinePath(e.dataDir))

	c, corpusErr := e.corpus()

	if fl.jsonOut {
		payload := map[string]any{
			"command":    "status",
			"data_dir":   e.dataDir,
			"has_index":  sc.HasIndex,
			"scope":      sc.payload(),
			"quarantine": quarantine,
		}
		if quarantine == nil {
			payload["quarantine"] = []index.QuarantineEntry{}
		}
		if qErr != nil {
			payload["quarantine_error"] = qErr.Error()
		}

		if c != nil && c.manifest != nil {
			m := c.manifest
			manifestMap := map[string]any{
				"embedding_model": m.EmbeddingModel,
				"dim":             m.Dim,
				"prefix_scheme":   m.PrefixScheme,
				"chunker_version": m.ChunkerVersion,
				"total_files":     len(m.Files),
				"total_chunks":    len(c.chunks),
				"roots":           m.Roots,
			}
			if hasManifest {
				manifestMap["last_sync"] = manifestInfo.ModTime().UTC().Format(time.RFC3339)
			}
			payload["manifest"] = manifestMap
		}

		if c != nil && c.store != nil {
			payload["db"] = map[string]any{
				"path":         index.StorePath(e.dataDir),
				"load_time_ms": c.dbLoadTime.Milliseconds(),
				"chunk_count":  c.store.Count(),
			}
		} else if hasManifest && corpusErr != nil {
			payload["db"] = map[string]any{
				"path":  index.StorePath(e.dataDir),
				"error": corpusErr.Error(),
			}
		}

		if sc.IndexError != "" {
			payload["index_error"] = sc.IndexError
		}

		return e.emitJSON(payload)
	}

	fmt.Fprintln(e.stdout, "Vektix Status")
	fmt.Fprintln(e.stdout, "=============")
	fmt.Fprintf(e.stdout, "Data Directory: %s\n", format.DisplayPath(e.dataDir))

	if !sc.HasIndex {
		if sc.IndexError != "" {
			fmt.Fprintf(e.stdout, "Index:          %s\n", sc.IndexError)
		} else {
			fmt.Fprintf(e.stdout, "Index:          No index found (run 'vektix index <dir>' to index files)\n")
		}
	} else {
		m := c.manifest
		fmt.Fprintf(e.stdout, "Index Chunks:   %s (%d files)\n", format.HumanInt(len(c.chunks)), len(m.Files))
		fmt.Fprintf(e.stdout, "Model:          %s (%d-dim, %s)\n", m.EmbeddingModel, m.Dim, m.PrefixScheme)
		fmt.Fprintf(e.stdout, "Chunker:        version %d\n", m.ChunkerVersion)
		if hasManifest {
			fmt.Fprintf(e.stdout, "Last Sync:      %s (%s ago)\n",
				manifestInfo.ModTime().Local().Format("2006-01-02 15:04:05"),
				format.FormatDuration(time.Since(manifestInfo.ModTime())))
		}
		fmt.Fprintln(e.stdout, "Indexed Roots:")
		if len(m.Roots) == 0 {
			fmt.Fprintln(e.stdout, "  (none)")
		} else {
			for _, r := range m.Roots {
				count := m.DirCounts[r]
				fmt.Fprintf(e.stdout, "  - %s (%s chunks)\n", format.DisplayPath(r), format.HumanInt(count))
			}
		}
		if c != nil && c.store != nil {
			fmt.Fprintf(e.stdout, "DB Load Time:   %s (%s chunks resident)\n", format.FormatDuration(c.dbLoadTime), format.HumanInt(c.store.Count()))
		} else if corpusErr != nil {
			fmt.Fprintf(e.stdout, "DB Load Time:   error loading store: %v\n", corpusErr)
		}
	}

	fmt.Fprintf(e.stdout, "Active Scope:   %s\n", sc.describe())
	if sc.Unindexed {
		fmt.Fprintf(e.stdout, "                (CWD %s is not under any indexed root)\n", format.DisplayPath(e.cwd))
	}

	if qErr != nil {
		fmt.Fprintf(e.stdout, "Quarantine:     error loading quarantine: %v\n", qErr)
	} else if len(quarantine) > 0 {
		fmt.Fprintf(e.stdout, "Quarantine:     %d quarantined file(s):\n", len(quarantine))
		for _, q := range quarantine {
			fmt.Fprintf(e.stdout, "  - %s: %s (%s)\n", format.DisplayPath(q.Path), q.Reason, q.Time.Local().Format("2006-01-02 15:04"))
		}
	} else {
		fmt.Fprintln(e.stdout, "Quarantine:     clean (0 files)")
	}

	return 0
}
