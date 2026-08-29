package tui

import (
	"testing"

	"github.com/Hendrixx-RE/Vektix/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

func TestPickerModel_NavigationAndSelection(t *testing.T) {
	p := NewPickerModel()
	if p.Active {
		t.Fatalf("expected inactive picker on creation")
	}

	items := []PickerItem{
		{Rank: 1, Path: "/path/to/first.go", Locator: store.Locator{Kind: store.LocatorLineRange, Start: 1, End: 10}},
		{Rank: 2, Path: "/path/to/second.go", Locator: store.Locator{Kind: store.LocatorLineRange, Start: 20, End: 30}},
		{Rank: 3, Path: "/path/to/third.go", Locator: store.Locator{Kind: store.LocatorLineRange, Start: 40, End: 50}},
	}

	p.Open(items, 80, 24)
	if !p.Active || len(p.Items) != 3 {
		t.Fatalf("expected 3 items and active picker")
	}

	// Initial selection
	sel, ok := p.Selected()
	if !ok || sel.Path != "/path/to/first.go" {
		t.Errorf("expected first item selected, got %+v", sel)
	}

	// Move down
	p, _, _, _ = p.Update(tea.KeyMsg{Type: tea.KeyDown})
	sel, _ = p.Selected()
	if sel.Path != "/path/to/second.go" {
		t.Errorf("expected second item selected after down key, got %s", sel.Path)
	}

	// Move down with 'j'
	p, _, _, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	sel, _ = p.Selected()
	if sel.Path != "/path/to/third.go" {
		t.Errorf("expected third item selected after j key, got %s", sel.Path)
	}

	// Wrap around down
	p, _, _, _ = p.Update(tea.KeyMsg{Type: tea.KeyDown})
	sel, _ = p.Selected()
	if sel.Path != "/path/to/first.go" {
		t.Errorf("expected wrap around to first item, got %s", sel.Path)
	}

	// Move up with 'k' -> wraps to third
	p, _, _, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	sel, _ = p.Selected()
	if sel.Path != "/path/to/third.go" {
		t.Errorf("expected wrap up to third item, got %s", sel.Path)
	}

	// Select with Enter
	var chosen *PickerItem
	p, _, chosen, ok = p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !ok || chosen == nil || chosen.Path != "/path/to/third.go" {
		t.Errorf("expected Enter to choose third item, got %+v, %v", chosen, ok)
	}
	if p.Active {
		t.Errorf("expected picker to close after Enter")
	}

	// Reopen and test quick pick with number key '2'
	p.Open(items, 80, 24)
	p, _, chosen, ok = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	if !ok || chosen == nil || chosen.Path != "/path/to/second.go" {
		t.Errorf("expected key '2' to choose second item, got %+v", chosen)
	}

	// Reopen and test cancellation with Esc
	p.Open(items, 80, 24)
	p, _, chosen, ok = p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if ok || chosen != nil || p.Active {
		t.Errorf("expected Esc to cancel and close picker")
	}
}

func TestPickerModel_ViewRendering(t *testing.T) {
	p := NewPickerModel()
	theme := DefaultTheme()
	if view := p.View(80, theme); view != "" {
		t.Errorf("expected empty view for inactive picker, got %q", view)
	}

	items := []PickerItem{
		{Rank: 1, Path: "/path/to/main.go", Locator: store.Locator{Kind: store.LocatorLineRange, Start: 10, End: 20}, Arms: []string{"path", "bm25"}},
	}
	p.Open(items, 80, 24)
	view := p.View(80, theme)
	if view == "" {
		t.Errorf("expected rendered picker box")
	}
}
