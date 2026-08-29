package tui

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hendrixx-RE/Vektix/internal/config"
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
	if app.scopeState.Global {
		t.Errorf("expected scoped state after :scope")
	}

	// 3. :global
	app.handleSubmit(":global")
	if !app.scopeState.Global {
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
	wasGlobal := app.scopeState.Global
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})

	if app.scopeState.Global == wasGlobal {
		t.Errorf("expected scope global state to toggle after 'g'")
	}
}
