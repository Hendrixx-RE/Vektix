package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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
	"github.com/Hendrixx-RE/Vektix/internal/router"
	"github.com/Hendrixx-RE/Vektix/internal/session"
	"github.com/Hendrixx-RE/Vektix/internal/store"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	singleArmRankCutoff = 5
	maxWeakMatches      = 3
	excerptLineBudget   = 12
	queryCacheSize      = 128
)

// AppMode defines the current UI interaction mode.
type AppMode int

const (
	ModeNormal AppMode = iota
	ModePicker
	ModeIndexing
)

// Messages used by the Bubble Tea loop
type searchCompleteMsg struct {
	Query     string
	Intent    *router.Intent
	Results   []SearchResult
	Weak      []SearchResult
	Notice    string
	ErrorMsg  string
	Warnings  []string
	Timestamp time.Time
}

type explainChunkMsg struct {
	EntryIndex int
	Chunk      string
}

type explainDoneMsg struct {
	EntryIndex int
	FullText   string
}

type explainErrMsg struct {
	EntryIndex int
	Err        error
}

type statusFlashMsg struct {
	Message string
	IsError bool
}

// AppModel is the root Bubble Tea model for the Vektix interactive TUI.
type AppModel struct {
	cfg         *config.Config
	dataDir     string
	cwd         string
	scopeState  ScopeState
	scopePinned string // explicit :scope override if set

	manifest     *index.Manifest
	store        *store.Store
	chunks       []store.Chunk
	pathIndex    *resolve.PathIndex
	bm25Index    *resolve.BM25Index
	vectorArm    *resolve.VectorArm
	ollamaClient *ollama.Client
	embedCache   *ollama.EmbeddingCache

	sessionRefs *session.Store

	input    textinput.Model
	viewport viewport.Model
	picker   PickerModel
	indexer  IndexModel

	history []ChatEntry
	mode    AppMode
	theme   Theme

	width  int
	height int
	ready  bool

	// Seams for testing
	copyFn func(w io.Writer, text string) (string, error)
	openFn func(path string, allowUnsafe bool, cfg *config.Config) error
}

// Options allows configuring the TUI on startup.
type Options struct {
	Config   *config.Config
	Cwd      string
	Scope    string
	Global   bool
	CopyFn   func(w io.Writer, text string) (string, error)
	OpenFn   func(path string, allowUnsafe bool, cfg *config.Config) error
}

// New creates and initializes a new AppModel.
func New(opts Options) (*AppModel, error) {
	cfg := opts.Config
	if cfg == nil {
		c, err := config.Load()
		if err != nil {
			return nil, fmt.Errorf("loading config: %w", err)
		}
		cfg = &c
	}

	dataDir, err := config.ExpandPath(cfg.General.DataDir)
	if err != nil {
		return nil, fmt.Errorf("resolving data_dir: %w", err)
	}

	cwd := opts.Cwd
	if cwd == "" {
		cwd, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("resolving cwd: %w", err)
		}
	}

	ti := textinput.New()
	ti.Placeholder = "Ask in plain English (e.g. 'where is my resume', 'open main.go', ':help')..."
	ti.Focus()
	ti.CharLimit = 512
	ti.Width = 80

	copyFunc := opts.CopyFn
	if copyFunc == nil {
		copyFunc = clipboard.CopyTo
	}

	openFunc := opts.OpenFn
	if openFunc == nil {
		openFunc = fileops.Open
	}

	app := &AppModel{
		cfg:         cfg,
		dataDir:     dataDir,
		cwd:         cwd,
		sessionRefs: session.NewStore(),
		input:       ti,
		picker:      NewPickerModel(),
		indexer:     NewIndexModel(),
		mode:        ModeNormal,
		theme:       DefaultTheme(),
		copyFn:      copyFunc,
		openFn:      openFunc,
	}

	app.loadCorpus()
	app.updateScope(opts.Scope, opts.Global)

	return app, nil
}

func (a *AppModel) countUnder(scope string) int {
	if scope == "" {
		return len(a.chunks)
	}
	n := 0
	for _, ch := range a.chunks {
		if isUnderScope(ch.Path, scope) {
			n++
		}
	}
	return n
}

func (a *AppModel) updateScope(scopeOverride string, forceGlobal bool) {
	a.scopePinned = scopeOverride
	a.scopeState = ResolveScopeState(a.cfg, a.cwd, scopeOverride, forceGlobal, len(a.chunks), a.countUnder)
	if a.manifest == nil {
		a.scopeState.HasIndex = false
		a.scopeState.IndexError = "no index found — run :index <path> or 'vektix index <path>'"
	}
}

func (a *AppModel) loadCorpus() {
	manifestPath := index.ManifestPath(a.dataDir)
	m, err := index.LoadManifest(manifestPath)
	if err != nil {
		a.manifest = nil
		a.chunks = nil
		return
	}
	a.manifest = m

	db, err := store.NewPersistentDB(index.StorePath(a.dataDir))
	if err != nil {
		a.store = nil
		a.chunks = nil
		return
	}
	a.store = db

	files := make([]string, 0, len(m.Files))
	for path := range m.Files {
		files = append(files, path)
	}
	sort.Strings(files)

	ctx := context.Background()
	a.chunks = make([]store.Chunk, 0)
	for _, path := range files {
		for _, id := range m.Files[path].Chunks {
			chunk, err := db.GetByID(ctx, id)
			if err != nil {
				continue
			}
			if chunk.Path == "" {
				chunk.Path = path
			}
			a.chunks = append(a.chunks, chunk)
		}
	}

	a.pathIndex = resolve.NewPathIndex(a.chunks)
	a.bm25Index = resolve.NewBM25Index(a.chunks)

	a.ollamaClient = ollama.NewClient(ollama.Options{
		Host:              a.cfg.Ollama.Host,
		EmbedTimeout:      time.Duration(a.cfg.Ollama.Timeouts.EmbedBatchSeconds) * time.Second,
		IntentTimeout:     time.Duration(a.cfg.Ollama.Timeouts.IntentSeconds) * time.Second,
		StreamIdleTimeout: time.Duration(a.cfg.Ollama.Timeouts.StreamIdleSeconds) * time.Second,
	})
	a.embedCache = ollama.NewEmbeddingCache(queryCacheSize)
	a.vectorArm = resolve.NewVectorArm(
		db,
		a.ollamaClient,
		a.embedCache,
		m,
		a.cfg.Ollama.EmbeddingModel,
		a.cfg.Ollama.KeepAlive,
		a.cfg.Search.OversampleFloor,
	)
}

// Init initializes the Bubble Tea loop.
func (a *AppModel) Init() tea.Cmd {
	return textinput.Blink
}

// Update processes Bubble Tea messages and keypresses.
func (a *AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		headerHeight := 2
		inputHeight := 3
		vpHeight := msg.Height - headerHeight - inputHeight
		if vpHeight < 4 {
			vpHeight = 4
		}

		if !a.ready {
			a.viewport = viewport.New(msg.Width, vpHeight)
			a.viewport.SetContent(RenderChat(a.history, msg.Width, a.theme))
			a.ready = true
		} else {
			a.viewport.Width = msg.Width
			a.viewport.Height = vpHeight
			a.viewport.SetContent(RenderChat(a.history, msg.Width, a.theme))
		}
		a.input.Width = msg.Width - 6
		return a, nil

	case IndexProgressMsg:
		var cmd tea.Cmd
		a.indexer, cmd = a.indexer.Update(msg)
		return a, cmd

	case IndexTickMsg:
		var cmd tea.Cmd
		a.indexer, cmd = a.indexer.Update(msg)
		return a, cmd

	case IndexDoneMsg:
		var cmd tea.Cmd
		a.indexer, cmd = a.indexer.Update(msg)
		if msg.Err == nil {
			a.loadCorpus()
			a.updateScope(a.scopePinned, a.scopeState.Global)
			a.sessionRefs.Clear()
		}
		return a, cmd

	case searchCompleteMsg:
		entry := ChatEntry{
			IsUser:    false,
			Query:     msg.Query,
			Intent:    msg.Intent,
			Results:   msg.Results,
			Notice:    msg.Notice,
			ErrorMsg:  msg.ErrorMsg,
			Timestamp: msg.Timestamp,
		}
		if len(msg.Warnings) > 0 {
			entry.WarningMsg = strings.Join(msg.Warnings, "\n")
		}

		a.history = append(a.history, entry)

		// Record results into session refs
		if len(msg.Results) > 0 {
			items := make([]session.Item, 0, len(msg.Results))
			for _, r := range msg.Results {
				items = append(items, session.Item{
					Rank:     r.Rank,
					Path:     r.Chunk.Path,
					Locator:  r.Locator,
					Content:  r.Text,
					Score:    r.Score,
					Arms:     r.Arms,
					BestRank: r.BestRank,
					Chunk:    &r.Chunk,
					Query:    msg.Query,
				})
			}
			a.sessionRefs.Set(msg.Query, items)
		}

		a.refreshViewport()
		return a, nil

	case explainChunkMsg:
		if msg.EntryIndex >= 0 && msg.EntryIndex < len(a.history) {
			a.history[msg.EntryIndex].ExplainContent += msg.Chunk
			a.history[msg.EntryIndex].ExplainLoading = false
			a.refreshViewport()
		}
		return a, nil

	case explainDoneMsg:
		if msg.EntryIndex >= 0 && msg.EntryIndex < len(a.history) {
			a.history[msg.EntryIndex].ExplainLoading = false
			a.refreshViewport()
		}
		return a, nil

	case explainErrMsg:
		if msg.EntryIndex >= 0 && msg.EntryIndex < len(a.history) {
			a.history[msg.EntryIndex].ExplainLoading = false
			a.history[msg.EntryIndex].ErrorMsg = fmt.Sprintf("explain failed: %v", msg.Err)
			a.refreshViewport()
		}
		return a, nil

	case statusFlashMsg:
		entry := ChatEntry{
			IsUser:    false,
			Timestamp: time.Now(),
		}
		if msg.IsError {
			entry.ErrorMsg = msg.Message
		} else {
			entry.SuccessMsg = msg.Message
		}
		a.history = append(a.history, entry)
		a.refreshViewport()
		return a, nil

	case tea.KeyMsg:
		// Global quit
		if msg.Type == tea.KeyCtrlC {
			return a, tea.Quit
		}

		// Handle Picker mode
		if a.mode == ModePicker {
			var pickedItem *PickerItem
			var ok bool
			var cmd tea.Cmd
			a.picker, cmd, pickedItem, ok = a.picker.Update(msg)
			if ok && pickedItem != nil {
				a.mode = ModeNormal
				a.activatePickerItem(*pickedItem)
			} else if !a.picker.Active {
				a.mode = ModeNormal
			}
			return a, cmd
		}

		// Handle Indexing mode
		if a.mode == ModeIndexing {
			if msg.String() == "esc" || msg.String() == "enter" {
				if !a.indexer.Running {
					a.mode = ModeNormal
					if a.indexer.Result != nil {
						a.history = append(a.history, ChatEntry{
							IsUser:     false,
							SuccessMsg: fmt.Sprintf("✓ Indexing finished: %d files, %d chunks", len(a.indexer.Result.Files), a.indexer.Result.Chunks),
							Timestamp:  time.Now(),
						})
					}
					a.refreshViewport()
					return a, nil
				}
			}
			var cmd tea.Cmd
			a.indexer, cmd = a.indexer.Update(msg)
			return a, cmd
		}

		// ModeNormal: Single-key hotkeys when input is empty OR ctrl hotkeys anytime
		isEmpty := len(a.input.Value()) == 0

		switch {
		case (isEmpty && msg.String() == "o") || msg.Type == tea.KeyCtrlO:
			return a, a.triggerOpenLatest()

		case (isEmpty && msg.String() == "c") || msg.Type == tea.KeyCtrlY:
			return a, a.triggerCopyLatest(false)

		case (isEmpty && msg.String() == "e") || msg.Type == tea.KeyCtrlE:
			return a, a.triggerExplainLatest()

		case (isEmpty && msg.String() == "n") || msg.Type == tea.KeyCtrlN:
			return a, a.triggerCycleMatch(1)

		case (isEmpty && msg.String() == "p") || msg.Type == tea.KeyCtrlP:
			return a, a.triggerCycleMatch(-1)

		case (isEmpty && msg.String() == "g") || msg.Type == tea.KeyCtrlG:
			return a, a.triggerToggleGlobal()

		case msg.Type == tea.KeyTab:
			return a, a.triggerOpenPicker()

		case msg.Type == tea.KeyCtrlL:
			a.history = nil
			a.refreshViewport()
			return a, nil

		case msg.Type == tea.KeyEnter:
			query := strings.TrimSpace(a.input.Value())
			if query != "" {
				a.input.SetValue("")
				return a, a.handleSubmit(query)
			}
			return a, nil

		case msg.Type == tea.KeyUp || msg.Type == tea.KeyDown || msg.Type == tea.KeyPgUp || msg.Type == tea.KeyPgDown:
			var vpCmd tea.Cmd
			a.viewport, vpCmd = a.viewport.Update(msg)
			return a, vpCmd
		}
	}

	var tiCmd tea.Cmd
	a.input, tiCmd = a.input.Update(msg)
	cmds = append(cmds, tiCmd)

	return a, tea.Batch(cmds...)
}

func (a *AppModel) refreshViewport() {
	if a.ready {
		a.viewport.SetContent(RenderChat(a.history, a.width, a.theme))
		a.viewport.GotoBottom()
	}
}

// handleSubmit executes a user query or in-TUI command.
func (a *AppModel) handleSubmit(input string) tea.Cmd {
	// Add user entry to history
	a.history = append(a.history, ChatEntry{
		IsUser:    true,
		Query:     input,
		Timestamp: time.Now(),
	})
	a.refreshViewport()

	// 1. Colon commands
	if strings.HasPrefix(input, ":") {
		return a.handleColonCommand(input)
	}

	// 2. Ordinal & pronoun session references ("open it", "copy that", "#2", "explain", "next")
	if cmd := a.handleSessionReferenceAction(input); cmd != nil {
		return cmd
	}

	// 3. Search / Router Intent
	return a.executeSearch(input)
}

func (a *AppModel) handleColonCommand(input string) tea.Cmd {
	parts := strings.Fields(input)
	cmd := parts[0]

	switch cmd {
	case ":q", ":quit", ":exit":
		return tea.Quit

	case ":clear":
		a.history = nil
		a.refreshViewport()
		return nil

	case ":help":
		helpText := strings.Join([]string{
			"🔷 Vektix Commands & Keybinds:",
			"  [o]pen        Open current match in editor",
			"  [c]opy        Copy current excerpt to clipboard",
			"  [e]xplain     Explain current passage with qwen2.5:3b (loaded on demand)",
			"  [n]ext        Cycle to next candidate match",
			"  [Tab]         Open candidate chooser picker",
			"  [g]lobal      Toggle between subtree scope and global search",
			"  :scope <dir>  Confine searches to <dir>",
			"  :scope global Search the whole index",
			"  :sync         Sync index and purge orphan chunks",
			"  :reindex      Rebuild index from scratch",
			"  :index <dir>  Index a new directory",
			"  :clear        Clear conversation history",
			"  :quit         Exit Vektix",
		}, "\n")
		a.history = append(a.history, ChatEntry{
			IsUser:    false,
			Notice:    helpText,
			Timestamp: time.Now(),
		})
		a.refreshViewport()
		return nil

	case ":scope":
		if len(parts) < 2 {
			a.history = append(a.history, ChatEntry{
				IsUser:    false,
				Notice:    fmt.Sprintf("Current active scope: %s", a.scopeState.Describe()),
				Timestamp: time.Now(),
			})
			a.refreshViewport()
			return nil
		}
		target := strings.Join(parts[1:], " ")
		if target == "global" || target == "-g" {
			a.updateScope("", true)
			a.sessionRefs.Clear()
			a.history = append(a.history, ChatEntry{
				IsUser:     false,
				SuccessMsg: fmt.Sprintf("✓ Switched scope to global (%s chunks). Session refs reset.", format.HumanInt(a.scopeState.Total)),
				Timestamp:  time.Now(),
			})
		} else {
			exp, err := config.ExpandPath(target)
			if err != nil {
				exp = target
			}
			a.updateScope(exp, false)
			a.sessionRefs.Clear()
			a.history = append(a.history, ChatEntry{
				IsUser:     false,
				SuccessMsg: fmt.Sprintf("✓ Switched scope to %s. Session refs reset.", a.scopeState.Describe()),
				Timestamp:  time.Now(),
			})
		}
		a.refreshViewport()
		return nil

	case ":global":
		a.updateScope("", true)
		a.sessionRefs.Clear()
		a.history = append(a.history, ChatEntry{
			IsUser:     false,
			SuccessMsg: fmt.Sprintf("✓ Switched scope to global (%s chunks). Session refs reset.", format.HumanInt(a.scopeState.Total)),
			Timestamp:  time.Now(),
		})
		a.refreshViewport()
		return nil

	case ":sync":
		a.mode = ModeIndexing
		return a.indexer.Start(a.cfg, nil, index.ModeSync)

	case ":reindex":
		a.mode = ModeIndexing
		return a.indexer.Start(a.cfg, nil, index.ModeReindex)

	case ":index":
		if len(parts) < 2 {
			a.history = append(a.history, ChatEntry{
				IsUser:    false,
				ErrorMsg:  "usage: :index <path>",
				Timestamp: time.Now(),
			})
			a.refreshViewport()
			return nil
		}
		dir := strings.Join(parts[1:], " ")
		exp, err := config.ExpandPath(dir)
		if err != nil {
			exp = dir
		}
		a.mode = ModeIndexing
		return a.indexer.Start(a.cfg, []string{exp}, index.ModeIndex)

	default:
		a.history = append(a.history, ChatEntry{
			IsUser:    false,
			ErrorMsg:  fmt.Sprintf("unknown command %q (type :help for commands)", cmd),
			Timestamp: time.Now(),
		})
		a.refreshViewport()
		return nil
	}
}

// handleSessionReferenceAction resolves actions like "open it", "copy that", "#2", "explain"
func (a *AppModel) handleSessionReferenceAction(input string) tea.Cmd {
	lower := strings.ToLower(strings.TrimSpace(input))

	// Quick action keywords
	if lower == "open" || lower == "open it" || lower == "open that" {
		return a.triggerOpenLatest()
	}
	if lower == "copy" || lower == "copy that" || lower == "copy it" {
		return a.triggerCopyLatest(false)
	}
	if lower == "copy path" {
		return a.triggerCopyLatest(true)
	}
	if lower == "explain" || lower == "explain it" || lower == "explain that" {
		return a.triggerExplainLatest()
	}
	if lower == "next" {
		return a.triggerCycleMatch(1)
	}
	if lower == "prev" || lower == "previous" {
		return a.triggerCycleMatch(-1)
	}

	// Pattern: "open <ref>" -> open specific ordinal ref
	if strings.HasPrefix(lower, "open ") {
		arg := strings.TrimSpace(lower[5:])
		if item, idx, ok := a.sessionRefs.ResolveRef(arg); ok {
			a.setActiveResultIndex(idx)
			return a.openItem(*item)
		}
	}

	// Pattern: "copy <ref>" -> copy specific ordinal ref
	if strings.HasPrefix(lower, "copy ") {
		arg := strings.TrimSpace(lower[5:])
		pathOnly := false
		if strings.HasPrefix(arg, "path ") {
			pathOnly = true
			arg = strings.TrimSpace(arg[5:])
		}
		if item, idx, ok := a.sessionRefs.ResolveRef(arg); ok {
			a.setActiveResultIndex(idx)
			return a.copyItem(*item, pathOnly)
		}
	}

	// Pattern: standalone explicit ordinal/pronoun reference like "#2", "the second one", "that pdf"
	if session.IsExplicitRef(lower) {
		if item, idx, ok := a.sessionRefs.ResolveRef(lower); ok {
			a.setActiveResultIndex(idx)
			a.history = append(a.history, ChatEntry{
				IsUser:     false,
				SuccessMsg: fmt.Sprintf("✓ Selected match #%d: %s", idx+1, format.DisplayPath(item.Path)),
				Timestamp:  time.Now(),
			})
			a.refreshViewport()
			return nil
		}
	}

	return nil
}

func (a *AppModel) setActiveResultIndex(idx int) {
	for i := len(a.history) - 1; i >= 0; i-- {
		if len(a.history[i].Results) > 0 {
			if idx >= 0 && idx < len(a.history[i].Results) {
				a.history[i].ActiveIndex = idx
				a.refreshViewport()
			}
			break
		}
	}
}

// executeSearch runs the 2-Tier router and executes retrieval.
func (a *AppModel) executeSearch(query string) tea.Cmd {
	return func() tea.Msg {
		if a.manifest == nil || len(a.chunks) == 0 {
			return searchCompleteMsg{
				Query:     query,
				ErrorMsg:  "no index found. Use ':index <path>' to index your files first.",
				Timestamp: time.Now(),
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		// Router Tier 1: Fast-path guarded regex
		intent := router.ParseFastPath(query)

		// Router Tier 2: LLM intent classification on fast-path miss
		if intent == nil && a.ollamaClient != nil {
			llmIntent, err := router.ParseLLM(ctx, a.ollamaClient, a.cfg.Ollama.IntentModel, query)
			if err == nil && llmIntent != nil {
				intent = llmIntent
			}
		}

		// Fallback intent if router miss or LLM failed
		if intent == nil {
			intent = &router.Intent{
				Action: "excerpt",
				Query:  query,
			}
		}

		// Execute based on intent action
		scope := a.scopeState.Path
		k := a.cfg.Search.MaxResults
		if k <= 0 {
			k = 8
		}

		searchQuery := intent.Query
		if searchQuery == "" {
			searchQuery = intent.Path
		}
		if searchQuery == "" {
			searchQuery = query
		}

		useVector := intent.Action == "excerpt"
		strong, weak, warnings := a.searchCorpus(ctx, searchQuery, scope, k, useVector)

		if len(strong) == 0 {
			emptyMsg := a.formatEmptyMessage(searchQuery, len(weak))
			return searchCompleteMsg{
				Query:     query,
				Intent:    intent,
				Notice:    emptyMsg,
				Weak:      weak,
				Warnings:  warnings,
				Timestamp: time.Now(),
			}
		}

		// Build formatted SearchResults
		var results []SearchResult
		for _, r := range strong {
			text, loc, err := a.excerptChunk(r.Chunk)
			if err != nil {
				continue
			}
			r.Text = text
			r.Locator = loc
			results = append(results, r)
		}

		if len(results) == 0 {
			return searchCompleteMsg{
				Query:     query,
				Intent:    intent,
				ErrorMsg:  fmt.Sprintf("matches were found in %s but could not be read from disk", a.scopeState.Describe()),
				Timestamp: time.Now(),
			}
		}

		return searchCompleteMsg{
			Query:     query,
			Intent:    intent,
			Results:   results,
			Weak:      weak,
			Warnings:  warnings,
			Timestamp: time.Now(),
		}
	}
}

type searchCandidate struct {
	chunk    store.Chunk
	score    float64
	arms     []string
	bestRank int
	rank     int
}

func (s searchCandidate) armLabel() string {
	return fmt.Sprintf("(%s, rank %d)", strings.Join(s.arms, "+"), s.rank)
}

func (a *AppModel) searchCorpus(ctx context.Context, query, scope string, k int, useVector bool) (strong, weak []SearchResult, warnings []string) {
	if query == "" || a.pathIndex == nil || a.bm25Index == nil {
		return nil, nil, nil
	}

	lists := []resolve.ResultList{
		a.pathIndex.Search(query, scope),
		a.bm25Index.Search(query, scope),
	}
	labels := []string{"path", "bm25"}

	fuse := func() ([]SearchResult, []SearchResult) {
		return a.classifyHits(lists, labels, scope, k)
	}

	strong, weak = fuse()
	if !useVector && len(strong) > 0 {
		return strong, weak, nil
	}

	if a.vectorArm != nil {
		vecK := k * 4
		if vecK < 20 {
			vecK = 20
		}
		vres, err := a.vectorArm.Search(ctx, query, scope, vecK)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("semantic search warning: %v", err))
		} else {
			lists = append(lists, vres)
			labels = append(labels, "vec")
			strong, weak = fuse()
		}
	}

	return strong, weak, warnings
}

func (a *AppModel) classifyHits(lists []resolve.ResultList, labels []string, scope string, k int) (strong, weak []SearchResult) {
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

	rrfK := a.cfg.Search.RRFK
	if rrfK <= 0 {
		rrfK = 60
	}
	minArms := a.cfg.Search.MinArms
	if minArms < 1 {
		minArms = 1
	}

	fused := resolve.Fuse(lists, rrfK, 1, 500)
	for _, sc := range fused {
		if !isUnderScope(sc.Path, scope) {
			continue
		}

		res := SearchResult{Chunk: sc.Chunk, Score: sc.Score, BestRank: 1 << 30}
		seen := map[string]bool{}
		for _, h := range hits[sc.ID] {
			if !seen[h.arm] {
				seen[h.arm] = true
				res.Arms = append(res.Arms, h.arm)
			}
			if h.rank < res.BestRank {
				res.BestRank = h.rank
			}
		}

		isStrong := len(res.Arms) >= minArms && (len(res.Arms) >= 2 || res.BestRank <= singleArmRankCutoff)
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
		strong[i].Rank = i + 1
		strong[i].ArmLabel = fmt.Sprintf("(%s, rank %d)", strings.Join(strong[i].Arms, "+"), strong[i].Rank)
	}
	for i := range weak {
		weak[i].Rank = i + 1
		weak[i].ArmLabel = fmt.Sprintf("(%s, rank %d)", strings.Join(weak[i].Arms, "+"), weak[i].Rank)
	}
	return strong, weak
}

func (a *AppModel) excerptChunk(chunk store.Chunk) (string, store.Locator, error) {
	if chunk.Locator.Kind == store.LocatorPage {
		return chunk.Content, chunk.Locator, nil
	}
	source, err := fileops.ReadFile(chunk.Path, false, a.cfg)
	if err != nil {
		return "", store.Locator{}, err
	}
	text, loc := excerpt.Expand(chunk, source, excerpt.ExpandConfig{MaxLines: excerptLineBudget})
	return text, loc, nil
}

func (a *AppModel) formatEmptyMessage(query string, weakHits int) string {
	if a.scopeState.Global {
		return fmt.Sprintf("no matches for %q in scope %s — run ':sync' or ':index <dir>' if files were added",
			query, a.scopeState.Describe())
	}
	if weakHits > 0 {
		return fmt.Sprintf("no matches for %q in scope %s (%d weak matches outside this scope — press [g] to search globally)",
			query, a.scopeState.Describe(), weakHits)
	}
	return fmt.Sprintf("no matches for %q in scope %s (press [g] to search all %s chunks globally)",
		query, a.scopeState.Describe(), format.HumanInt(a.scopeState.Total))
}

// Keybind Action Triggers

func (a *AppModel) triggerOpenLatest() tea.Cmd {
	item, ok := a.getActiveItem()
	if !ok {
		return a.flashStatus("no search results to open", true)
	}
	return a.openItem(item)
}

func (a *AppModel) triggerCopyLatest(pathOnly bool) tea.Cmd {
	item, ok := a.getActiveItem()
	if !ok {
		return a.flashStatus("no search results to copy", true)
	}
	return a.copyItem(item, pathOnly)
}

func (a *AppModel) triggerExplainLatest() tea.Cmd {
	item, ok := a.getActiveItem()
	if !ok {
		return a.flashStatus("no excerpt to explain", true)
	}

	modelName := a.cfg.Ollama.ExplainModel
	if modelName == "" {
		modelName = "qwen2.5:3b-instruct"
	}

	entryIdx := len(a.history)
	a.history = append(a.history, ChatEntry{
		IsUser:         false,
		ExplainLoading: true,
		ExplainModel:   modelName,
		Timestamp:      time.Now(),
	})
	a.refreshViewport()

	return a.streamExplain(entryIdx, item, modelName)
}

func (a *AppModel) streamExplain(entryIdx int, item session.Item, modelName string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		promptText := fmt.Sprintf("File: %s\n```\n%s\n```\nExplain what this code or document section does and why it is significant.",
			format.DisplayPath(item.Path), item.Content)

		numCtx := a.cfg.Ollama.Context.ExplainNumCtx
		if numCtx <= 0 {
			numCtx = 8192
		}

		req := ollama.ChatRequest{
			Model: modelName,
			Messages: []ollama.Message{
				{Role: "system", Content: "You are Vektix. Explain the provided file excerpt clearly, concisely, and accurately."},
				{Role: "user", Content: promptText},
			},
			Options: map[string]any{
				"num_ctx":     numCtx,
				"num_predict": 300,
				"temperature": 0,
				"seed":        1,
			},
		}

		resp, err := a.ollamaClient.Chat(ctx, req)
		if err != nil {
			return explainErrMsg{
				EntryIndex: entryIdx,
				Err:        err,
			}
		}

		return explainChunkMsg{
			EntryIndex: entryIdx,
			Chunk:      resp.Message.Content,
		}
	}
}

func (a *AppModel) triggerCycleMatch(delta int) tea.Cmd {
	for i := len(a.history) - 1; i >= 0; i-- {
		if len(a.history[i].Results) > 0 {
			total := len(a.history[i].Results)
			next := (a.history[i].ActiveIndex + delta) % total
			if next < 0 {
				next += total
			}
			a.history[i].ActiveIndex = next
			a.refreshViewport()
			return nil
		}
	}
	return a.flashStatus("no search results to cycle", true)
}

func (a *AppModel) triggerToggleGlobal() tea.Cmd {
	newGlobal := !a.scopeState.Global
	a.updateScope(a.scopePinned, newGlobal)
	a.sessionRefs.Clear()

	msg := fmt.Sprintf("✓ Switched scope to global (%s chunks). Session refs reset.", format.HumanInt(a.scopeState.Total))
	if !newGlobal {
		msg = fmt.Sprintf("✓ Switched scope to %s. Session refs reset.", a.scopeState.Describe())
	}
	return a.flashStatus(msg, false)
}

func (a *AppModel) triggerOpenPicker() tea.Cmd {
	for i := len(a.history) - 1; i >= 0; i-- {
		if len(a.history[i].Results) > 1 {
			var pickerItems []PickerItem
			for _, r := range a.history[i].Results {
				pickerItems = append(pickerItems, PickerItem{
					Rank:     r.Rank,
					Path:     r.Chunk.Path,
					Locator:  r.Locator,
					Content:  r.Text,
					Score:    r.Score,
					Arms:     r.Arms,
					BestRank: r.BestRank,
					Chunk:    &r.Chunk,
				})
			}
			a.picker.Open(pickerItems, a.width, a.height)
			a.mode = ModePicker
			return nil
		}
	}
	return a.flashStatus("no ambiguous candidates to pick from", true)
}

func (a *AppModel) activatePickerItem(item PickerItem) {
	for i := len(a.history) - 1; i >= 0; i-- {
		if len(a.history[i].Results) > 0 {
			for idx, r := range a.history[i].Results {
				if r.Chunk.ID == item.Chunk.ID || r.Chunk.Path == item.Path {
					a.history[i].ActiveIndex = idx
					break
				}
			}
			break
		}
	}
	a.refreshViewport()
}

func (a *AppModel) getActiveItem() (session.Item, bool) {
	for i := len(a.history) - 1; i >= 0; i-- {
		if len(a.history[i].Results) > 0 {
			idx := a.history[i].ActiveIndex
			if idx < 0 || idx >= len(a.history[i].Results) {
				idx = 0
			}
			r := a.history[i].Results[idx]
			return session.Item{
				Rank:     r.Rank,
				Path:     r.Chunk.Path,
				Locator:  r.Locator,
				Content:  r.Text,
				Score:    r.Score,
				Arms:     r.Arms,
				BestRank: r.BestRank,
				Chunk:    &r.Chunk,
				Query:    a.history[i].Query,
			}, true
		}
	}

	if a.sessionRefs.Count() > 0 {
		it, ok := a.sessionRefs.Get(0)
		if ok && it != nil {
			return *it, true
		}
	}

	return session.Item{}, false
}

func (a *AppModel) openItem(item session.Item) tea.Cmd {
	err := a.openFn(item.Path, false, a.cfg)
	if err != nil {
		return a.flashStatus(fmt.Sprintf("could not open %s: %v", format.DisplayPath(item.Path), err), true)
	}

	locInfo := ""
	if item.Locator.Start > 0 {
		locInfo = fmt.Sprintf(":%d", item.Locator.Start)
	}
	return a.flashStatus(fmt.Sprintf("✓ opened %s%s in editor", format.DisplayPath(item.Path), locInfo), false)
}

func (a *AppModel) copyItem(item session.Item, pathOnly bool) tea.Cmd {
	payload := format.DisplayPath(item.Path)
	modeDesc := "path"
	if !pathOnly {
		payload = item.Content
		modeDesc = "excerpt"
	}

	mechanism, err := a.copyFn(os.Stderr, payload)
	if err != nil {
		return a.flashStatus(fmt.Sprintf("clipboard copy failed: %v", err), true)
	}

	return a.flashStatus(fmt.Sprintf("✓ copied %s of %s to clipboard (%s)", modeDesc, format.DisplayPath(item.Path), mechanism), false)
}

func (a *AppModel) flashStatus(msg string, isError bool) tea.Cmd {
	return func() tea.Msg {
		return statusFlashMsg{Message: msg, IsError: isError}
	}
}

// View renders the TUI layout.
func (a *AppModel) View() string {
	if !a.ready {
		return "Initializing Vektix..."
	}

	header := RenderStatusBar(a.width, a.scopeState, a.theme)

	var mainView string
	switch a.mode {
	case ModePicker:
		mainView = lipgloss.JoinVertical(
			lipgloss.Left,
			a.viewport.View(),
			a.picker.View(a.width, a.theme),
		)
	case ModeIndexing:
		mainView = lipgloss.JoinVertical(
			lipgloss.Left,
			a.viewport.View(),
			a.indexer.View(a.width, a.theme),
		)
	default:
		mainView = a.viewport.View()
	}

	inputBox := lipgloss.JoinHorizontal(
		lipgloss.Left,
		a.theme.Prompt.Render("> "),
		a.input.View(),
	)

	divider := a.theme.Gutter.Render(strings.Repeat("─", a.width))

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		divider,
		mainView,
		divider,
		inputBox,
	)
}

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
