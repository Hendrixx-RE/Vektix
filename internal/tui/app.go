package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	"github.com/charmbracelet/bubbles/spinner"
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
	maxChatHistory      = 200
)

// AppMode defines the current UI interaction mode.
type AppMode int

const (
	ModeNormal AppMode = iota
	ModePicker
	ModeIndexing
)

// corpusState holds the thread-safe snapshot of index structures.
type corpusState struct {
	manifest     *index.Manifest
	store        *store.Store
	chunks       []store.Chunk
	pathIndex    *resolve.PathIndex
	bm25Index    *resolve.BM25Index
	vectorArm    *resolve.VectorArm
	ollamaClient *ollama.Client
	embedCache   *ollama.EmbeddingCache
}

type corpusHolder struct {
	mu         sync.RWMutex
	state      *corpusState
	scopeState ScopeState
}

// AppModel represents the root Elm architecture state for Vektix TUI.
type AppModel struct {
	cfg         *config.Config
	cwd         string
	width       int
	height      int
	mode        AppMode
	input       textinput.Model
	viewport    viewport.Model
	history     []ChatEntry
	sessionRefs *session.Store
	picker      PickerModel
	indexer     IndexModel
	scopePinned string
	theme       Theme
	statusFlash string
	statusIsErr bool
	statusTimer time.Time
	corpus      *corpusHolder
	copyFn      func(w io.Writer, text string) (string, error)
	openFn      func(path string, allowUnsafe bool, cfg *config.Config) error
}

// Options provides initialization options for creating the AppModel.
type Options struct {
	Config      *config.Config
	Cwd         string
	ScopeTarget string
	Global      bool
	CopyFn      func(w io.Writer, text string) (string, error)
	OpenFn      func(path string, allowUnsafe bool, cfg *config.Config) error
}

// New initializes an AppModel with the provided configuration and options.
func New(opts Options) (*AppModel, error) {
	cfg := opts.Config
	if cfg == nil {
		c := config.DefaultConfig()
		cfg = &c
	}

	ti := textinput.New()
	ti.Placeholder = "Ask where files are, search passages, or type :help..."
	ti.Focus()
	ti.CharLimit = 512
	ti.Width = 80

	vp := viewport.New(80, 20)

	sessionStore := session.NewStore()
	theme := DefaultTheme()

	app := &AppModel{
		cfg:         cfg,
		cwd:         opts.Cwd,
		mode:        ModeNormal,
		input:       ti,
		viewport:    vp,
		history:     make([]ChatEntry, 0),
		sessionRefs: sessionStore,
		picker:      NewPickerModel(),
		indexer:     NewIndexModel(),
		theme:       theme,
		scopePinned: opts.ScopeTarget,
		corpus:      &corpusHolder{},
		copyFn:      opts.CopyFn,
		openFn:      opts.OpenFn,
	}

	if app.copyFn == nil {
		app.copyFn = clipboard.CopyTo
	}
	if app.openFn == nil {
		app.openFn = fileops.Open
	}

	app.loadCorpus()
	app.updateScope(opts.ScopeTarget, opts.Global)

	return app, nil
}

func (a *AppModel) getCorpus() *corpusState {
	if a.corpus == nil {
		return nil
	}
	a.corpus.mu.RLock()
	defer a.corpus.mu.RUnlock()
	return a.corpus.state
}

func (a *AppModel) setCorpus(c *corpusState) {
	if a.corpus == nil {
		a.corpus = &corpusHolder{}
	}
	a.corpus.mu.Lock()
	defer a.corpus.mu.Unlock()
	a.corpus.state = c
}

func (a *AppModel) getScopeState() ScopeState {
	if a.corpus == nil {
		return ScopeState{}
	}
	a.corpus.mu.RLock()
	defer a.corpus.mu.RUnlock()
	return a.corpus.scopeState
}

func (a *AppModel) setScopeState(st ScopeState) {
	if a.corpus != nil {
		a.corpus.mu.Lock()
		a.corpus.scopeState = st
		a.corpus.mu.Unlock()
	}
}

// loadCorpus loads or reloads the index from data_dir in a thread-safe manner.
func (a *AppModel) loadCorpus() {
	dataDir, err := config.ExpandPath(a.cfg.General.DataDir)
	if err != nil {
		st := a.getScopeState()
		st.IndexError = "cannot resolve data_dir: " + err.Error()
		a.setScopeState(st)
		return
	}

	manifest, err := index.LoadManifest(index.ManifestPath(dataDir))
	if err != nil {
		st := a.getScopeState()
		st.IndexError = "no index found in " + format.DisplayPath(dataDir)
		a.setScopeState(st)
		return
	}

	if manifest.EmbeddingModel == "" || manifest.Dim == 0 {
		st := a.getScopeState()
		st.IndexError = "incomplete manifest header (run ':reindex' to rebuild)"
		a.setScopeState(st)
		return
	}
	if err := manifest.CheckValidity(a.cfg.Ollama.EmbeddingModel, manifest.Dim, manifest.PrefixScheme, manifest.ChunkerVersion); err != nil {
		st := a.getScopeState()
		st.IndexError = fmt.Sprintf("index schema mismatch (indexed with %q, config says %q)", manifest.EmbeddingModel, a.cfg.Ollama.EmbeddingModel)
		a.setScopeState(st)
		return
	}

	st, err := store.NewPersistentDB(index.StorePath(dataDir))
	if err != nil {
		sc := a.getScopeState()
		sc.IndexError = "opening store: " + err.Error()
		a.setScopeState(sc)
		return
	}

	files := make([]string, 0, len(manifest.Files))
	for path := range manifest.Files {
		files = append(files, path)
	}
	sort.Strings(files)

	var allIDs []string
	idToPath := make(map[string]string)
	for _, path := range files {
		for _, id := range manifest.Files[path].Chunks {
			allIDs = append(allIDs, id)
			idToPath[id] = path
		}
	}

	allChunks, _ := st.GetByIDs(context.Background(), allIDs)
	for i := range allChunks {
		if allChunks[i].Path == "" {
			allChunks[i].Path = idToPath[allChunks[i].ID]
		}
	}

	pathIdx := resolve.NewPathIndex(allChunks)
	bm25Idx := resolve.NewBM25Index(allChunks)

	cli := ollama.NewClient(ollama.Options{
		Host:              a.cfg.Ollama.Host,
		EmbedTimeout:      time.Duration(a.cfg.Ollama.Timeouts.EmbedBatchSeconds) * time.Second,
		IntentTimeout:     time.Duration(a.cfg.Ollama.Timeouts.IntentSeconds) * time.Second,
		StreamIdleTimeout: time.Duration(a.cfg.Ollama.Timeouts.StreamIdleSeconds) * time.Second,
	})
	cache := ollama.NewEmbeddingCache(queryCacheSize)

	vectorArm := resolve.NewVectorArm(
		st,
		cli,
		cache,
		manifest,
		a.cfg.Ollama.EmbeddingModel,
		a.cfg.Ollama.KeepAlive,
		a.cfg.Search.OversampleFloor,
	)

	newCorpus := &corpusState{
		manifest:     manifest,
		store:        st,
		chunks:       allChunks,
		pathIndex:    pathIdx,
		bm25Index:    bm25Idx,
		vectorArm:    vectorArm,
		ollamaClient: cli,
		embedCache:   cache,
	}

	a.setCorpus(newCorpus)
	sc := a.getScopeState()
	sc.IndexError = ""
	a.setScopeState(sc)
}

func (a *AppModel) updateScope(override string, global bool) {
	c := a.getCorpus()
	totalChunks := 0
	if c != nil {
		totalChunks = len(c.chunks)
	}

	countFn := func(scope string) int {
		if c == nil {
			return 0
		}
		if scope == "" {
			return len(c.chunks)
		}
		count := 0
		for _, ch := range c.chunks {
			if isUnderScope(ch.Path, scope) {
				count++
			}
		}
		return count
	}

	st := ResolveScopeState(a.cfg, a.cwd, override, global, totalChunks, countFn)
	a.setScopeState(st)
}

func isUnderScope(path, scope string) bool {
	if scope == "" {
		return true
	}
	cleanScope := filepath.Clean(scope)
	cleanPath := filepath.Clean(path)
	if cleanPath == cleanScope {
		return true
	}
	return strings.HasPrefix(cleanPath, cleanScope+string(filepath.Separator))
}

// Init initializes Bubble Tea sub-components.
func (a *AppModel) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, a.checkBackgroundReconcileCmd())
}

type backgroundReconcileStartMsg struct{}
type backgroundReconcileDoneMsg struct {
	res *index.Result
	err error
}

func (a *AppModel) checkBackgroundReconcileCmd() tea.Cmd {
	return func() tea.Msg {
		dataDir, err := config.ExpandPath(a.cfg.General.DataDir)
		if err != nil {
			return nil
		}
		m, err := index.LoadManifest(index.ManifestPath(dataDir))
		if err != nil || m == nil || len(m.Roots) == 0 {
			return nil
		}
		stale, err := m.IsStale(&a.cfg.Index)
		if err != nil || !stale {
			return nil
		}
		return backgroundReconcileStartMsg{}
	}
}

func (a *AppModel) runBackgroundReconcileCmd() tea.Cmd {
	return func() tea.Msg {
		dataDir, err := config.ExpandPath(a.cfg.General.DataDir)
		if err != nil {
			return backgroundReconcileDoneMsg{err: err}
		}
		st, err := store.NewPersistentDB(index.StorePath(dataDir))
		if err != nil {
			return backgroundReconcileDoneMsg{err: err}
		}
		cli := ollama.NewClient(ollama.Options{
			Host:         a.cfg.Ollama.Host,
			EmbedTimeout: time.Duration(a.cfg.Ollama.Timeouts.EmbedBatchSeconds) * time.Second,
		})
		m, err := index.LoadManifest(index.ManifestPath(dataDir))
		if err != nil || m == nil {
			return backgroundReconcileDoneMsg{err: err}
		}

		engine := index.NewEngine(a.cfg, st, cli, dataDir)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		res, runErr := engine.Run(ctx, m.Roots, index.ModeSync)
		return backgroundReconcileDoneMsg{res: res, err: runErr}
	}
}

type searchCompleteMsg struct {
	Query     string
	Intent    *router.Intent
	Results   []SearchResult
	Notice    string
	Weak      []SearchResult
	Warnings  []string
	ErrorMsg  string
	Timestamp time.Time
}

type explainChunkMsg struct {
	EntryIndex int
	Chunk      string
}

type explainErrMsg struct {
	EntryIndex int
	Err        error
}

type clearFlashMsg struct{}

func flashClearCmd() tea.Cmd {
	return tea.Tick(4*time.Second, func(t time.Time) tea.Msg {
		return clearFlashMsg{}
	})
}

// Update handles message dispatch for the Elm loop.
func (a *AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		headerHeight := 2
		inputHeight := 3
		statusBarHeight := 1
		a.viewport.Width = msg.Width
		a.viewport.Height = msg.Height - headerHeight - inputHeight - statusBarHeight
		if a.viewport.Height < 4 {
			a.viewport.Height = 4
		}
		a.input.Width = msg.Width - 6
		return a, nil

	case backgroundReconcileStartMsg:
		st := a.getScopeState()
		st.Reconciling = true
		a.setScopeState(st)
		return a, a.runBackgroundReconcileCmd()

	case backgroundReconcileDoneMsg:
		st := a.getScopeState()
		st.Reconciling = false
		a.setScopeState(st)
		if msg.err == nil && msg.res != nil {
			a.loadCorpus()
			a.updateScope(a.scopePinned, a.getScopeState().Global)
			if msg.res.Added > 0 || msg.res.Updated > 0 || msg.res.Removed > 0 {
				a.sessionRefs.Clear()
				a.refreshViewport()
			}
		}
		return a, nil

	case IndexProgressMsg:
		cmd := a.indexer.Update(msg)
		return a, cmd

	case spinner.TickMsg:
		cmd := a.indexer.Update(msg)
		return a, cmd

	case IndexDoneMsg:
		cmd := a.indexer.Update(msg)
		if msg.Err == nil {
			a.loadCorpus()
			if a.scopePinned == "" && !a.getScopeState().Global {
				a.updateScope(a.cwd, false)
			} else {
				a.updateScope(a.scopePinned, a.getScopeState().Global)
			}
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

		a.appendHistory(entry)

		// Record in session refs
		if len(msg.Results) > 0 {
			var items []session.Item
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
			a.history[msg.EntryIndex].ExplainLoading = false
			a.history[msg.EntryIndex].ExplainContent = msg.Chunk
			a.refreshViewport()
		}
		return a, nil

	case explainErrMsg:
		if msg.EntryIndex >= 0 && msg.EntryIndex < len(a.history) {
			a.history[msg.EntryIndex].ExplainLoading = false
			a.history[msg.EntryIndex].ErrorMsg = fmt.Sprintf("explain error: %v", msg.Err)
			a.refreshViewport()
		}
		return a, nil

	case clearFlashMsg:
		if time.Since(a.statusTimer) >= 3800*time.Millisecond {
			a.statusFlash = ""
			a.statusIsErr = false
		}
		return a, nil

	case tea.KeyMsg:
		// Global Quit
		if msg.Type == tea.KeyCtrlC {
			return a, tea.Quit
		}

		// Handle Picker mode
		if a.mode == ModePicker {
			targetIdx := a.picker.TargetHistoryIndex
			var item *PickerItem
			var picked bool
			a.picker, _, item, picked = a.picker.Update(msg)
			if !a.picker.Active {
				a.mode = ModeNormal
			}
			if picked && item != nil {
				a.activatePickerItem(targetIdx, *item)
			}
			return a, nil
		}

		// Handle Indexing mode: Esc or Enter cancels and immediately returns to normal mode
		if a.mode == ModeIndexing {
			if msg.String() == "esc" || msg.String() == "enter" {
				if a.indexer.Running && a.indexer.CancelFn != nil {
					a.indexer.CancelFn()
					a.indexer.CancelFn = nil
					a.indexer.Running = false
				}
				a.mode = ModeNormal
				a.refreshViewport()
				return a, nil
			}
			cmd := a.indexer.Update(msg)
			return a, cmd
		}

		// Single-key hotkeys when text input is empty
		if a.input.Value() == "" {
			switch msg.String() {
			case "o":
				return a, a.triggerOpenLatest()
			case "c":
				return a, a.triggerCopyLatest(false)
			case "e":
				return a, a.triggerExplainLatest()
			case "n":
				return a, a.triggerCycleMatch(1)
			case "p":
				return a, a.triggerCycleMatch(-1)
			case "g":
				return a, a.triggerToggleGlobal()
			case "tab":
				return a, a.triggerOpenPicker()
			case "esc":
				return a, tea.Quit
			}
		}

		// Submit input with Enter
		if msg.Type == tea.KeyEnter {
			val := strings.TrimSpace(a.input.Value())
			if val == "" {
				return a, nil
			}
			a.input.SetValue("")
			cmd := a.handleSubmit(val)
			return a, cmd
		}

		// Forward arrow navigation to viewport if relevant
		switch msg.Type {
		case tea.KeyUp, tea.KeyDown, tea.KeyPgUp, tea.KeyPgDown:
			var vpCmd tea.Cmd
			a.viewport, vpCmd = a.viewport.Update(msg)
			cmds = append(cmds, vpCmd)
		}
	}

	var inputCmd tea.Cmd
	a.input, inputCmd = a.input.Update(msg)
	cmds = append(cmds, inputCmd)

	return a, tea.Batch(cmds...)
}

func (a *AppModel) appendHistory(entry ChatEntry) {
	a.history = append(a.history, entry)
	if len(a.history) > maxChatHistory {
		a.history = a.history[len(a.history)-maxChatHistory:]
	}
}

func (a *AppModel) refreshViewport() {
	rendered := RenderChat(a.history, a.viewport.Width, a.theme)
	a.viewport.SetContent(rendered)
	a.viewport.GotoBottom()
}

// handleSubmit routes colon commands, NL session refs, or new search queries.
func (a *AppModel) handleSubmit(input string) tea.Cmd {
	// Add user query to history
	a.appendHistory(ChatEntry{
		IsUser:    true,
		Query:     input,
		Timestamp: time.Now(),
	})

	// 1. Colon Command Dispatch
	if strings.HasPrefix(input, ":") {
		return a.handleColonCommand(input)
	}

	// 2. Session Reference Action (e.g. "open that", "copy it", "#2")
	if cmd := a.handleSessionReferenceAction(input); cmd != nil {
		return cmd
	}

	// 3. New Intent/Search Execution
	a.refreshViewport()
	return a.executeIntent(input)
}

func (a *AppModel) handleColonCommand(input string) tea.Cmd {
	parts := strings.Fields(input)
	cmd := parts[0]

	switch cmd {
	case ":q", ":quit", ":exit":
		return tea.Quit

	case ":help", ":?":
		helpText := strings.Join([]string{
			"🔷 Vektix Commands & Keybinds",
			"  [o]pen      Open current result in editor",
			"  [c]opy      Copy current excerpt to clipboard",
			"  [e]xplain   Explain current excerpt with Ollama",
			"  [n]ext      Cycle to next matching result",
			"  [g]lobal    Toggle global search on / off",
			"  [tab]       Open ambiguous candidate picker",
			"",
			"Commands:",
			"  :scope <path>    Switch active search scope",
			"  :global          Search across all indexed roots",
			"  :index [dir]     Index or reindex directories",
			"  :index-here      Index current directory ephemerally (transient)",
			"  :sync            Sync and purge orphan chunks",
			"  :status          Show active scope and chunk counts",
			"  :clear           Clear query and chat history",
			"  :help            Show this help dialog",
			"  :quit            Exit Vektix",
		}, "\n")

		a.appendHistory(ChatEntry{
			IsUser:    false,
			Notice:    helpText,
			Timestamp: time.Now(),
		})
		a.refreshViewport()
		return nil

	case ":clear":
		a.history = nil
		a.sessionRefs.Clear()
		a.refreshViewport()
		return nil

	case ":status":
		a.appendHistory(ChatEntry{
			IsUser:    false,
			Notice:    a.getScopeState().Banner(),
			Timestamp: time.Now(),
		})
		a.refreshViewport()
		return nil

	case ":scope":
		if len(parts) < 2 {
			a.appendHistory(ChatEntry{
				IsUser:    false,
				Notice:    fmt.Sprintf("Current active scope: %s", a.getScopeState().Describe()),
				Timestamp: time.Now(),
			})
			a.refreshViewport()
			return nil
		}
		target := strings.Join(parts[1:], " ")
		if target == "global" || target == "-g" {
			a.updateScope("", true)
			a.sessionRefs.Clear()
			a.appendHistory(ChatEntry{
				IsUser:     false,
				SuccessMsg: fmt.Sprintf("✓ Switched scope to global (%s chunks). Session refs reset.", format.HumanInt(a.getScopeState().Total)),
				Timestamp:  time.Now(),
			})
		} else {
			exp, err := config.ExpandPath(target)
			if err != nil {
				exp = target
			}
			a.updateScope(exp, false)
			a.sessionRefs.Clear()
			a.appendHistory(ChatEntry{
				IsUser:     false,
				SuccessMsg: fmt.Sprintf("✓ Switched scope to %s. Session refs reset.", a.getScopeState().Describe()),
				Timestamp:  time.Now(),
			})
		}
		a.refreshViewport()
		return nil

	case ":global":
		a.updateScope("", true)
		a.sessionRefs.Clear()
		a.appendHistory(ChatEntry{
			IsUser:     false,
			SuccessMsg: fmt.Sprintf("✓ Switched scope to global (%s chunks). Session refs reset.", format.HumanInt(a.getScopeState().Total)),
			Timestamp:  time.Now(),
		})
		a.refreshViewport()
		return nil

	case ":index":
		roots := a.cfg.Index.IndexDirs
		if len(parts) > 1 {
			target := strings.Join(parts[1:], " ")
			exp, err := config.ExpandPath(target)
			if err != nil {
				exp = target
			}
			roots = []string{exp}
		}
		a.mode = ModeIndexing
		return a.indexer.Start(a.cfg, roots, index.ModeIndex)

	case ":index-here", ":index-transient":
		a.mode = ModeIndexing
		return a.indexer.StartTransient(a.cfg, a.cwd)

	case ":sync":
		roots := a.cfg.Index.IndexDirs
		a.mode = ModeIndexing
		return a.indexer.Start(a.cfg, roots, index.ModeSync)

	case ":reindex":
		roots := a.cfg.Index.IndexDirs
		a.mode = ModeIndexing
		return a.indexer.Start(a.cfg, roots, index.ModeReindex)

	default:
		a.appendHistory(ChatEntry{
			IsUser:    false,
			ErrorMsg:  fmt.Sprintf("unknown command: %s (type :help for available commands)", cmd),
			Timestamp: time.Now(),
		})
		a.refreshViewport()
		return nil
	}
}

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

	// Pattern: standalone explicit ordinal/pronoun reference like "#2", "2", "the second one", "that pdf"
	if session.IsExplicitRef(lower) {
		if item, idx, ok := a.sessionRefs.ResolveRef(lower); ok {
			a.setActiveResultIndex(idx)
			a.appendHistory(ChatEntry{
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
				a.sessionRefs.SetActiveIndex(idx)
				a.refreshViewport()
			}
			break
		}
	}
}

// executeIntent runs the 2-Tier router and dispatches on intent.Action.
func (a *AppModel) executeIntent(query string) tea.Cmd {
	return func() tea.Msg {
		c := a.getCorpus()
		if c == nil || c.manifest == nil || len(c.chunks) == 0 {
			return searchCompleteMsg{
				Query:     query,
				ErrorMsg:  "no index found. Use ':index <path>' to index your files first.",
				Timestamp: time.Now(),
			}
		}

		scopeState := a.getScopeState()
		scope := scopeState.Path

		// Touch transient root if querying within it
		if scope != "" && c.manifest.TouchPath(scope) {
			if dataDir, err := config.ExpandPath(a.cfg.General.DataDir); err == nil {
				_ = c.manifest.SaveManifest(index.ManifestPath(dataDir))
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		// Router Tier 1: Fast-path guarded regex
		intent := router.ParseFastPath(query)

		// Router Tier 2: LLM intent classification on fast-path miss
		if intent == nil && c.ollamaClient != nil {
			llmIntent, err := router.ParseLLM(ctx, c.ollamaClient, a.cfg.Ollama.IntentModel, query)
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

		k := a.cfg.Search.MaxResults
		if k <= 0 {
			k = 8
		}

		switch intent.Action {
		case "open":
			targetPath := intent.Path
			if targetPath == "" {
				targetPath = intent.Query
			}
			if targetPath == "" {
				if it, ok := a.sessionRefs.Get(a.sessionRefs.ActiveIndex()); ok {
					targetPath = it.Path
				}
			}

			resolvedPath := a.resolveLocalOrSearchPath(c, ctx, targetPath, scope)
			if resolvedPath != "" {
				err := a.openFn(resolvedPath, false, a.cfg)
				if err != nil {
					return searchCompleteMsg{
						Query:     query,
						Intent:    intent,
						ErrorMsg:  fmt.Sprintf("could not open %s: %v", format.DisplayPath(resolvedPath), err),
						Timestamp: time.Now(),
					}
				}
				return searchCompleteMsg{
					Query:     query,
					Intent:    intent,
					Notice:    fmt.Sprintf("✓ opened %s in editor", format.DisplayPath(resolvedPath)),
					Timestamp: time.Now(),
				}
			}

			return searchCompleteMsg{
				Query:     query,
				Intent:    intent,
				ErrorMsg:  fmt.Sprintf("could not find file to open matching %q", targetPath),
				Timestamp: time.Now(),
			}

		case "copy":
			target := intent.Path
			if target == "" {
				target = intent.Query
			}
			pathOnly := strings.Contains(strings.ToLower(query), "path")

			targetPath := target
			content := ""
			if target == "" {
				if it, ok := a.sessionRefs.Get(a.sessionRefs.ActiveIndex()); ok {
					targetPath = it.Path
					content = it.Content
				}
			} else {
				resolvedPath := a.resolveLocalOrSearchPath(c, ctx, target, scope)
				if resolvedPath != "" {
					targetPath = resolvedPath
					data, readErr := fileops.ReadFile(resolvedPath, false, a.cfg)
					if readErr == nil {
						content = string(data)
					}
				}
			}

			if targetPath == "" {
				return searchCompleteMsg{
					Query:     query,
					Intent:    intent,
					ErrorMsg:  "no search results or file to copy",
					Timestamp: time.Now(),
				}
			}

			payload := format.DisplayPath(targetPath)
			modeDesc := "path"
			if !pathOnly && content != "" {
				payload = content
				modeDesc = "excerpt"
			}

			mechanism, err := a.copyFn(os.Stderr, payload)
			if err != nil {
				return searchCompleteMsg{
					Query:     query,
					Intent:    intent,
					ErrorMsg:  fmt.Sprintf("clipboard copy failed: %v", err),
					Timestamp: time.Now(),
				}
			}

			return searchCompleteMsg{
				Query:     query,
				Intent:    intent,
				Notice:    fmt.Sprintf("✓ copied %s of %s to clipboard (%s)", modeDesc, format.DisplayPath(targetPath), mechanism),
				Timestamp: time.Now(),
			}

		case "read":
			rawTarget := intent.Path
			if rawTarget == "" {
				rawTarget = intent.Query
			}
			targetPath, startLine, endLine := splitPathRange(rawTarget)
			if intent.Lines != "" {
				if s, e, err := parseLineRange(intent.Lines); err == nil {
					startLine = s
					endLine = e
				}
			}

			resolvedPath := a.resolveLocalOrSearchPath(c, ctx, targetPath, scope)
			if resolvedPath == "" {
				return searchCompleteMsg{
					Query:     query,
					Intent:    intent,
					ErrorMsg:  fmt.Sprintf("could not find file %q to read", targetPath),
					Timestamp: time.Now(),
				}
			}

			data, err := fileops.ReadFile(resolvedPath, false, a.cfg)
			if err != nil {
				return searchCompleteMsg{
					Query:     query,
					Intent:    intent,
					ErrorMsg:  fmt.Sprintf("could not read %s: %v", format.DisplayPath(resolvedPath), err),
					Timestamp: time.Now(),
				}
			}

			content := string(data)
			lines := strings.Split(content, "\n")
			start := 1
			end := len(lines)
			if startLine > 0 {
				start = startLine
			}
			if endLine > 0 {
				end = endLine
			}
			if start > len(lines) {
				start = len(lines)
			}
			if end > len(lines) {
				end = len(lines)
			}
			if start < 1 {
				start = 1
			}

			var body strings.Builder
			for i := start - 1; i < end; i++ {
				body.WriteString(fmt.Sprintf("%4d │ %s\n", i+1, lines[i]))
			}

			header := fmt.Sprintf("%s:%d-%d (%d lines)\n", format.DisplayPath(resolvedPath), start, end, end-start+1)
			return searchCompleteMsg{
				Query:     query,
				Intent:    intent,
				Notice:    header + "\n" + body.String(),
				Timestamp: time.Now(),
			}

		case "list":
			dir := intent.Path
			if dir == "" {
				dir = scope
			}
			if dir == "" {
				dir = a.cwd
			}

			if !filepath.IsAbs(dir) && a.cwd != "" {
				cwdPath := filepath.Join(a.cwd, dir)
				if _, err := os.Stat(cwdPath); err == nil {
					dir = cwdPath
				}
			}

			safeDir, err := fileops.ResolvePath(dir, false, a.cfg)
			if err != nil {
				return searchCompleteMsg{
					Query:     query,
					Intent:    intent,
					ErrorMsg:  fmt.Sprintf("could not list %s: %v", format.DisplayPath(dir), err),
					Timestamp: time.Now(),
				}
			}

			entries, err := os.ReadDir(safeDir)
			if err != nil {
				return searchCompleteMsg{
					Query:     query,
					Intent:    intent,
					ErrorMsg:  fmt.Sprintf("reading directory %s: %v", format.DisplayPath(safeDir), err),
					Timestamp: time.Now(),
				}
			}

			var out strings.Builder
			out.WriteString(fmt.Sprintf("📁 Listing for %s:\n", format.DisplayPath(safeDir)))
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), ".") {
					continue
				}
				if e.IsDir() {
					out.WriteString(fmt.Sprintf("  📂 %s/\n", e.Name()))
				} else {
					info, _ := e.Info()
					sz := ""
					if info != nil {
						sz = fmt.Sprintf(" (%s)", format.HumanBytes(info.Size()))
					}
					out.WriteString(fmt.Sprintf("  📄 %s%s\n", e.Name(), sz))
				}
			}

			return searchCompleteMsg{
				Query:     query,
				Intent:    intent,
				Notice:    out.String(),
				Timestamp: time.Now(),
			}

		case "locate":
			searchQuery := intent.Query
			if searchQuery == "" {
				searchQuery = intent.Path
			}
			if searchQuery == "" {
				searchQuery = query
			}

			strong, weak, warnings := a.searchCorpusWith(c, ctx, searchQuery, scope, k, false)
			if len(strong) == 0 {
				emptyMsg := a.formatEmptyMessage(searchQuery, len(weak), scopeState)
				return searchCompleteMsg{
					Query:     query,
					Intent:    intent,
					Notice:    emptyMsg,
					Weak:      weak,
					Warnings:  warnings,
					Timestamp: time.Now(),
				}
			}

			// Deduplicate locate results by path
			seenPaths := map[string]bool{}
			var deduped []SearchResult
			for _, r := range strong {
				if !seenPaths[r.Chunk.Path] {
					seenPaths[r.Chunk.Path] = true
					r.Text = format.DisplayPath(r.Chunk.Path)
					deduped = append(deduped, r)
				}
			}

			return searchCompleteMsg{
				Query:     query,
				Intent:    intent,
				Results:   deduped,
				Weak:      weak,
				Warnings:  warnings,
				Timestamp: time.Now(),
			}

		default: // "excerpt" and general search
			searchQuery := intent.Query
			if searchQuery == "" {
				searchQuery = intent.Path
			}
			if searchQuery == "" {
				searchQuery = query
			}

			strong, weak, warnings := a.searchCorpusWith(c, ctx, searchQuery, scope, k, true)
			if len(strong) == 0 {
				emptyMsg := a.formatEmptyMessage(searchQuery, len(weak), scopeState)
				return searchCompleteMsg{
					Query:     query,
					Intent:    intent,
					Notice:    emptyMsg,
					Weak:      weak,
					Warnings:  warnings,
					Timestamp: time.Now(),
				}
			}

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
					ErrorMsg:  fmt.Sprintf("matches were found in %s but could not be read from disk", scopeState.Describe()),
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
}

func (a *AppModel) searchCorpusWith(c *corpusState, ctx context.Context, query, scope string, k int, useVector bool) (strong, weak []SearchResult, warnings []string) {
	if query == "" || c == nil || c.pathIndex == nil || c.bm25Index == nil {
		return nil, nil, nil
	}

	lists := []resolve.ResultList{
		c.pathIndex.Search(query, scope),
		c.bm25Index.Search(query, scope),
	}
	labels := []string{"path", "bm25"}

	fuse := func() ([]SearchResult, []SearchResult) {
		return a.classifyHits(lists, labels, scope, k)
	}

	strong, weak = fuse()
	if !useVector && len(strong) > 0 {
		return strong, weak, nil
	}

	if c.vectorArm != nil {
		vecK := k * 4
		if vecK < 20 {
			vecK = 20
		}
		vres, err := c.vectorArm.Search(ctx, query, scope, vecK)
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

func (a *AppModel) resolveLocalOrSearchPath(c *corpusState, ctx context.Context, target, scope string) string {
	if target == "" {
		return ""
	}
	// 1. If relative, check under cwd
	if !filepath.IsAbs(target) && a.cwd != "" {
		cwdPath := filepath.Join(a.cwd, target)
		if _, err := os.Stat(cwdPath); err == nil {
			if safe, err := fileops.ResolvePath(cwdPath, false, a.cfg); err == nil {
				return safe
			}
		}
	}
	// 2. Direct ResolvePath
	if safe, err := fileops.ResolvePath(target, false, a.cfg); err == nil {
		if _, err := os.Stat(safe); err == nil {
			return safe
		}
	}
	// 3. Fallback to corpus search
	strong, _, _ := a.searchCorpusWith(c, ctx, target, scope, 1, false)
	if len(strong) > 0 {
		return strong[0].Chunk.Path
	}
	return ""
}

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

func parseLineRange(spec string) (int, int, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return 0, 0, fmt.Errorf("empty line range")
	}
	if !strings.Contains(spec, "-") {
		n, err := strconv.Atoi(spec)
		if err != nil || n < 1 {
			return 0, 0, fmt.Errorf("invalid line range %q", spec)
		}
		return n, n, nil
	}
	parts := strings.SplitN(spec, "-", 2)
	var start, end int
	var err error
	if parts[0] != "" {
		if start, err = strconv.Atoi(parts[0]); err != nil || start < 1 {
			return 0, 0, fmt.Errorf("invalid line range %q", spec)
		}
	} else {
		start = 1
	}
	if parts[1] != "" {
		if end, err = strconv.Atoi(parts[1]); err != nil || end < 1 {
			return 0, 0, fmt.Errorf("invalid line range %q", spec)
		}
	}
	if end != 0 && end < start {
		return 0, 0, fmt.Errorf("invalid line range %q: end before start", spec)
	}
	return start, end, nil
}

func (a *AppModel) formatEmptyMessage(query string, weakHits int, st ScopeState) string {
	if st.Global {
		return fmt.Sprintf("no matches for %q in scope %s — run ':sync' or ':index <dir>' if files were added",
			query, st.Describe())
	}
	if weakHits > 0 {
		return fmt.Sprintf("no matches for %q in scope %s (%d weak matches outside this scope — press [g] to search globally)",
			query, st.Describe(), weakHits)
	}
	return fmt.Sprintf("no matches for %q in scope %s (press [g] to search all %s chunks globally)",
		query, st.Describe(), format.HumanInt(st.Total))
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

	a.appendHistory(ChatEntry{
		IsUser:         false,
		ExplainLoading: true,
		ExplainModel:   modelName,
		Timestamp:      time.Now(),
	})
	entryIdx := len(a.history) - 1
	a.refreshViewport()

	return a.streamExplain(entryIdx, item, modelName)
}

func (a *AppModel) streamExplain(entryIdx int, item session.Item, modelName string) tea.Cmd {
	return func() tea.Msg {
		c := a.getCorpus()
		if c == nil || c.ollamaClient == nil {
			return explainErrMsg{
				EntryIndex: entryIdx,
				Err:        fmt.Errorf("ollama client not available"),
			}
		}

		timeout := time.Duration(a.cfg.Ollama.Timeouts.StreamIdleSeconds) * time.Second
		if timeout <= 0 {
			timeout = 60 * time.Second
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
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

		resp, err := c.ollamaClient.Chat(ctx, req)
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
			a.sessionRefs.SetActiveIndex(next)
			a.refreshViewport()
			return nil
		}
	}
	return a.flashStatus("no search results to cycle", true)
}

func (a *AppModel) triggerToggleGlobal() tea.Cmd {
	st := a.getScopeState()
	newGlobal := !st.Global
	a.updateScope(a.scopePinned, newGlobal)
	a.sessionRefs.Clear()

	st = a.getScopeState()
	msg := fmt.Sprintf("✓ Switched scope to global (%s chunks). Session refs reset.", format.HumanInt(st.Total))
	if !newGlobal {
		msg = fmt.Sprintf("✓ Switched scope to %s. Session refs reset.", st.Describe())
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
			a.picker.Open(i, pickerItems, a.width, a.height)
			a.mode = ModePicker
			return nil
		}
	}
	return a.flashStatus("no ambiguous candidates to pick from", true)
}

func (a *AppModel) activatePickerItem(targetHistoryIdx int, item PickerItem) {
	if targetHistoryIdx >= 0 && targetHistoryIdx < len(a.history) {
		for idx, r := range a.history[targetHistoryIdx].Results {
			if r.Chunk.ID == item.Chunk.ID || r.Chunk.Path == item.Path {
				a.history[targetHistoryIdx].ActiveIndex = idx
				a.sessionRefs.SetActiveIndex(idx)
				break
			}
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
		it, ok := a.sessionRefs.Get(a.sessionRefs.ActiveIndex())
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
		a.statusFlash = msg
		a.statusIsErr = isError
		a.statusTimer = time.Now()
		return clearFlashMsg{}
	}
}

// View renders the complete TUI layout.
func (a *AppModel) View() string {
	width := a.width
	if width < 40 {
		width = 80
	}

	// 1. Top Status Bar
	statusBar := RenderStatusBar(width, a.getScopeState(), a.theme)

	// 2. Center Content Area (Viewport or Picker or Indexing)
	var centerContent string
	switch a.mode {
	case ModePicker:
		centerContent = a.picker.View(width, a.theme)
	case ModeIndexing:
		centerContent = a.indexer.View(width, a.theme)
	default:
		centerContent = a.viewport.View()
	}

	// 3. Status Flash / Error Notice
	var flashLine string
	if a.statusFlash != "" {
		if a.statusIsErr {
			flashLine = a.theme.ErrorText.Render("✗ " + a.statusFlash)
		} else {
			flashLine = a.theme.SuccessText.Render(a.statusFlash)
		}
	}

	// 4. Bottom Input Box
	inputBox := lipgloss.NewStyle().MarginTop(1).Render(
		lipgloss.JoinHorizontal(
			lipgloss.Left,
			a.theme.Prompt.Render("❯ "),
			a.input.View(),
		),
	)

	// Compose Layout
	elements := []string{statusBar}
	if flashLine != "" {
		elements = append(elements, flashLine)
	}
	elements = append(elements, centerContent, inputBox)

	return lipgloss.JoinVertical(lipgloss.Left, elements...)
}
