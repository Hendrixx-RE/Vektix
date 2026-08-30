package tui

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hendrixx-RE/Vektix/internal/config"
	"github.com/Hendrixx-RE/Vektix/internal/index"
	"github.com/Hendrixx-RE/Vektix/internal/resolve"
	"github.com/Hendrixx-RE/Vektix/internal/router"
	"github.com/Hendrixx-RE/Vektix/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

func newTestApp(t *testing.T) (*AppModel, *string, *string) {
	t.Helper()
	dir := t.TempDir()

	cfg := config.DefaultConfig()
	cfg.General.DataDir = dir
	cfg.Index.IndexDirs = []string{dir}

	var copiedText string
	var openedPath string

	app, err := New(Options{
		Config: &cfg,
		Cwd:    dir,
		CopyFn: func(w io.Writer, text string) (string, error) {
			copiedText = text
			return "test-copy", nil
		},
		OpenFn: func(path string, allowUnsafe bool, c *config.Config) error {
			openedPath = path
			return nil
		},
	})
	if err != nil {
		t.Fatalf("failed to create AppModel: %v", err)
	}

	// Initialize window size
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	return app, &copiedText, &openedPath
}

func TestApp_WindowResizeAndInit(t *testing.T) {
	app, _, _ := newTestApp(t)

	cmd := app.Init()
	if cmd == nil {
		t.Errorf("expected Init() to return a blinking cursor command")
	}

	view := app.View()
	if !strings.Contains(view, "VEKTIX") {
		t.Errorf("expected view to contain VEKTIX header")
	}
}

func TestApp_ColonCommands(t *testing.T) {
	app, _, _ := newTestApp(t)

	// 1. :help
	app.handleSubmit(":help")
	if len(app.history) != 2 { // user entry + notice
		t.Fatalf("expected 2 history entries, got %d", len(app.history))
	}
	if !strings.Contains(app.history[1].Notice, "Commands & Keybinds") {
		t.Errorf("unexpected help notice: %s", app.history[1].Notice)
	}

	// 2. :scope <path>
	subDir := filepath.Join(app.cwd, "sub")
	_ = os.MkdirAll(subDir, 0755)
	app.handleSubmit(":scope " + subDir)
	if app.getScopeState().Global {
		t.Errorf("expected scoped state after :scope")
	}

	// 3. :global
	app.handleSubmit(":global")
	if !app.getScopeState().Global {
		t.Errorf("expected global state after :global")
	}

	// 4. :clear
	app.handleSubmit(":clear")
	if len(app.history) != 0 {
		t.Errorf("expected empty history after :clear, got %d", len(app.history))
	}
}

func TestApp_SearchResultsAndActions(t *testing.T) {
	app, copiedText, openedPath := newTestApp(t)

	// Simulate search result message arriving
	testResults := []SearchResult{
		{
			Chunk: store.Chunk{
				ID:      "c1",
				Path:    "/path/to/server.go",
				Content: "func StartServer() { listen() }",
				Locator: store.Locator{Kind: store.LocatorLineRange, Start: 40, End: 45},
			},
			Text:     "func StartServer() { listen() }",
			Locator:  store.Locator{Kind: store.LocatorLineRange, Start: 40, End: 45},
			Rank:     1,
			Arms:     []string{"path", "bm25"},
			ArmLabel: "(path+bm25, rank 1)",
		},
		{
			Chunk: store.Chunk{
				ID:      "c2",
				Path:    "/path/to/client.go",
				Content: "func NewClient() *Client",
				Locator: store.Locator{Kind: store.LocatorLineRange, Start: 10, End: 15},
			},
			Text:     "func NewClient() *Client",
			Locator:  store.Locator{Kind: store.LocatorLineRange, Start: 10, End: 15},
			Rank:     2,
			Arms:     []string{"vec"},
			ArmLabel: "(vec, rank 2)",
		},
	}

	app.Update(searchCompleteMsg{
		Query:     "server start",
		Intent:    &router.Intent{Action: "excerpt", Query: "server start"},
		Results:   testResults,
		Timestamp: time.Now(),
	})

	if app.sessionRefs.Count() != 2 {
		t.Fatalf("expected 2 session items, got %d", app.sessionRefs.Count())
	}

	// Test hotkey 'o' (open current)
	app.input.SetValue("")
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if *openedPath != "/path/to/server.go" {
		t.Errorf("expected open /path/to/server.go, got %s", *openedPath)
	}

	// Test hotkey 'c' (copy current excerpt)
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if !strings.Contains(*copiedText, "func StartServer()") {
		t.Errorf("expected copied excerpt text, got %q", *copiedText)
	}

	// Test hotkey 'n' (cycle to next match)
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	// Now active index should be 1 (/path/to/client.go)
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if *openedPath != "/path/to/client.go" {
		t.Errorf("expected open /path/to/client.go after cycle, got %s", *openedPath)
	}

	// Test natural language ordinal command: "open the first one"
	app.handleSubmit("open the first one")
	if *openedPath != "/path/to/server.go" {
		t.Errorf("expected open /path/to/server.go via NL ordinal, got %s", *openedPath)
	}

	// Test natural language command: "copy that"
	app.handleSubmit("copy that")
	if *copiedText == "" {
		t.Errorf("expected copy via 'copy that'")
	}

	// Test natural language bare digit selection: "2"
	app.handleSubmit("2")
	if app.sessionRefs.ActiveIndex() != 1 {
		t.Errorf("expected active index 1 after selecting '2', got %d", app.sessionRefs.ActiveIndex())
	}

	// Test that a plain search query like "server" is NOT hijacked as a session ref selection
	cmd := app.handleSessionReferenceAction("server")
	if cmd != nil {
		t.Errorf("plain query 'server' should NOT be intercepted as session reference action")
	}
}

func TestApp_PickerIntegration(t *testing.T) {
	app, _, openedPath := newTestApp(t)

	testResults := []SearchResult{
		{
			Chunk: store.Chunk{ID: "c1", Path: "/path/to/one.go"},
			Text:  "one",
			Rank:  1,
		},
		{
			Chunk: store.Chunk{ID: "c2", Path: "/path/to/two.go"},
			Text:  "two",
			Rank:  2,
		},
	}

	app.Update(searchCompleteMsg{
		Query:     "search",
		Results:   testResults,
		Timestamp: time.Now(),
	})

	// Press Tab to open picker
	app.Update(tea.KeyMsg{Type: tea.KeyTab})
	if app.mode != ModePicker || !app.picker.Active {
		t.Fatalf("expected ModePicker active after Tab key")
	}

	// Navigate down with j
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	// Select with Enter
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if app.mode != ModeNormal {
		t.Errorf("expected return to ModeNormal after picker selection")
	}

	// Verify active index is now 1
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if *openedPath != "/path/to/two.go" {
		t.Errorf("expected open /path/to/two.go after picker select, got %s", *openedPath)
	}
}

func TestApp_GlobalToggleKey(t *testing.T) {
	app, _, _ := newTestApp(t)

	// Press 'g' when input is empty to toggle global
	app.input.SetValue("")
	wasGlobal := app.getScopeState().Global
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})

	if app.getScopeState().Global == wasGlobal {
		t.Errorf("expected scope global state to toggle after 'g'")
	}
}

func TestApp_IndexProgressAndAnimation(t *testing.T) {
	app, _, _ := newTestApp(t)

	app.mode = ModeIndexing
	app.indexer.Running = true

	// Progress message arrives
	app.Update(IndexProgressMsg{
		Progress: index.Progress{Scanned: 50, Indexed: 10, Chunks: 35},
	})
	if app.indexer.Progress.Scanned != 50 || app.indexer.Progress.Chunks != 35 {
		t.Errorf("expected index progress updated in model, got %+v", app.indexer.Progress)
	}

	// Spinner tick arrives
	app.Update(IndexTickMsg{})
	if app.indexer.SpinnerIndex != 1 {
		t.Errorf("expected spinnerIndex 1 after tick, got %d", app.indexer.SpinnerIndex)
	}

	// Done message arrives
	app.Update(IndexDoneMsg{
		Result: &index.Result{
			Files:  []string{"/a/b.go"},
			Chunks: 5,
		},
	})
	if app.indexer.Running {
		t.Errorf("expected indexer running = false after IndexDoneMsg")
	}
}

func TestApp_IndexCancelKey(t *testing.T) {
	app, _, _ := newTestApp(t)

	app.mode = ModeIndexing
	app.indexer.Running = true
	canceled := false
	app.indexer.CancelFn = func() {
		canceled = true
	}

	// Press esc during indexing
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if !canceled {
		t.Errorf("expected cancel function to be called on Esc")
	}
	if app.mode != ModeNormal {
		t.Errorf("expected mode to reset to ModeNormal on Esc, got %v", app.mode)
	}
	if app.indexer.Running {
		t.Errorf("expected indexer running to be false")
	}
}

func TestApp_IntentActionExecution(t *testing.T) {
	app, copiedText, openedPath := newTestApp(t)

	// Create real file in app cwd
	testFile := filepath.Join(app.cwd, "server.go")
	_ = os.WriteFile(testFile, []byte("package main\n\nfunc Run() {\n\tprintln(\"hello\")\n}\n"), 0644)

	subDir := filepath.Join(app.cwd, "docs")
	_ = os.MkdirAll(subDir, 0755)
	_ = os.WriteFile(filepath.Join(subDir, "readme.md"), []byte("# Readme"), 0644)

	// Set up corpus with test file
	chunks := []store.Chunk{
		{ID: "c1", Path: testFile, Content: "package main\nfunc Run()"},
	}
	app.setCorpus(&corpusState{
		manifest:  &index.Manifest{Files: map[string]index.FileMeta{testFile: {Chunks: []string{"c1"}}}},
		chunks:    chunks,
		pathIndex: resolve.NewPathIndex(chunks),
		bm25Index: resolve.NewBM25Index(chunks),
	})

	// 1. Action: "open server.go"
	cmd := app.executeIntent("open server.go")
	msg := cmd()
	app.Update(msg)
	if *openedPath != testFile {
		t.Errorf("expected executeIntent('open server.go') to open %s, got %s", testFile, *openedPath)
	}

	// 2. Action: "copy server.go"
	cmd = app.executeIntent("copy server.go")
	msg = cmd()
	app.Update(msg)
	if *copiedText == "" {
		t.Errorf("expected executeIntent('copy server.go') to copy file content")
	}

	// 3. Action: "read server.go:1-3"
	cmd = app.executeIntent("read server.go:1-3")
	msg = cmd()
	scMsg, ok := msg.(searchCompleteMsg)
	if !ok || !strings.Contains(scMsg.Notice, "package main") {
		t.Errorf("expected executeIntent('read server.go:1-3') to return content, got %+v", msg)
	}

	// 4. Action: "list docs"
	cmd = app.executeIntent("list docs")
	msg = cmd()
	scMsg, ok = msg.(searchCompleteMsg)
	if !ok || !strings.Contains(scMsg.Notice, "readme.md") {
		t.Errorf("expected executeIntent('list docs') to list readme.md, got %+v", msg)
	}
}

func TestApp_ConcurrentLoadCorpusAndSearch(t *testing.T) {
	app, _, _ := newTestApp(t)

	// Set test corpus data
	app.setCorpus(&corpusState{
		manifest: &index.Manifest{},
		chunks: []store.Chunk{
			{ID: "c1", Path: "/a/b.go", Content: "package b"},
		},
	})

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			cmd := app.executeIntent("package b")
			_ = cmd()
		}()
		go func() {
			defer wg.Done()
			app.loadCorpus()
		}()
	}
	wg.Wait()
}

func TestApp_IndexHereCommand(t *testing.T) {
	app, _, _ := newTestApp(t)

	// Issue :index-here command
	cmd := app.handleSubmit(":index-here")
	if cmd == nil {
		t.Fatalf("expected cmd from :index-here")
	}
	if app.mode != ModeIndexing {
		t.Errorf("expected ModeIndexing after :index-here, got %v", app.mode)
	}
	if !app.indexer.Running {
		t.Errorf("expected indexer to be running after :index-here")
	}
}

func TestApp_BackgroundReconcileState(t *testing.T) {
	app, _, _ := newTestApp(t)

	// Simulate background reconcile start message
	app.Update(backgroundReconcileStartMsg{})
	if !app.getScopeState().Reconciling {
		t.Errorf("expected Reconciling = true after backgroundReconcileStartMsg")
	}

	// Verify status bar renders the reconciling indicator
	rendered := RenderStatusBar(100, app.getScopeState(), app.theme)
	if !strings.Contains(rendered, "syncing") {
		t.Errorf("expected status bar to contain 'syncing' during reconcile, got: %s", rendered)
	}

	// Simulate background reconcile done message
	app.Update(backgroundReconcileDoneMsg{
		res: &index.Result{Unchanged: 5},
	})
	if app.getScopeState().Reconciling {
		t.Errorf("expected Reconciling = false after backgroundReconcileDoneMsg")
	}
}
