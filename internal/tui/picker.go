package tui

import (
	"fmt"
	"strings"

	"github.com/Hendrixx-RE/Vektix/internal/format"
	"github.com/Hendrixx-RE/Vektix/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// PickerItem represents a selectable candidate in the ambiguous match picker.
type PickerItem struct {
	Rank     int
	Path     string
	Locator  store.Locator
	Content  string
	Score    float64
	Arms     []string
	BestRank int
	Chunk    *store.Chunk
}

// PickerModel manages keyboard selection among multiple candidate search results.
type PickerModel struct {
	Items              []PickerItem
	Cursor             int
	Active             bool
	Width              int
	Height             int
	TargetHistoryIndex int
}

// NewPickerModel creates an inactive picker.
func NewPickerModel() PickerModel {
	return PickerModel{
		Items:              make([]PickerItem, 0),
		Cursor:             0,
		Active:             false,
		TargetHistoryIndex: -1,
	}
}

// Open activates the picker with the provided candidate items and target entry index.
func (p *PickerModel) Open(targetHistoryIndex int, items []PickerItem, width, height int) {
	p.Items = items
	p.Cursor = 0
	p.Active = len(items) > 0
	p.Width = width
	p.Height = height
	p.TargetHistoryIndex = targetHistoryIndex
}

// Close deactivates the picker.
func (p *PickerModel) Close() {
	p.Active = false
	p.Items = nil
	p.Cursor = 0
	p.TargetHistoryIndex = -1
}

// Selected returns the currently highlighted candidate.
func (p *PickerModel) Selected() (*PickerItem, bool) {
	if !p.Active || len(p.Items) == 0 || p.Cursor < 0 || p.Cursor >= len(p.Items) {
		return nil, false
	}
	item := p.Items[p.Cursor]
	return &item, true
}

// SelectIndex sets the cursor and returns the chosen item if valid.
func (p *PickerModel) SelectIndex(idx int) (*PickerItem, bool) {
	if !p.Active || idx < 0 || idx >= len(p.Items) {
		return nil, false
	}
	p.Cursor = idx
	item := p.Items[p.Cursor]
	return &item, true
}

// Update handles keypresses for candidate list navigation.
func (p PickerModel) Update(msg tea.Msg) (PickerModel, tea.Cmd, *PickerItem, bool) {
	if !p.Active {
		return p, nil, nil, false
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if p.Cursor > 0 {
				p.Cursor--
			} else {
				p.Cursor = len(p.Items) - 1
			}
			return p, nil, nil, false

		case "down", "j":
			if p.Cursor < len(p.Items)-1 {
				p.Cursor++
			} else {
				p.Cursor = 0
			}
			return p, nil, nil, false

		case "enter":
			item, ok := p.Selected()
			p.Close()
			return p, nil, item, ok

		case "esc", "q":
			p.Close()
			return p, nil, nil, false

		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			num := int(msg.String()[0] - '1')
			if num < len(p.Items) {
				item := p.Items[num]
				p.Close()
				return p, nil, &item, true
			}
		}
	}

	return p, nil, nil, false
}

// View renders the candidate picker box with styled options.
func (p PickerModel) View(width int, theme Theme) string {
	if !p.Active || len(p.Items) == 0 {
		return ""
	}

	var rows []string
	rows = append(rows, theme.PickerTitle.Render("Select candidate:"))

	maxVisible := 8
	start := 0
	if p.Cursor >= maxVisible {
		start = p.Cursor - maxVisible + 1
	}
	end := start + maxVisible
	if end > len(p.Items) {
		end = len(p.Items)
	}

	for i := start; i < end; i++ {
		item := p.Items[i]
		locStr := ""
		if item.Locator.Kind == store.LocatorLineRange && item.Locator.Start > 0 {
			locStr = fmt.Sprintf(":%d-%d", item.Locator.Start, item.Locator.End)
		} else if item.Locator.Kind == store.LocatorPage && item.Locator.Start > 0 {
			locStr = fmt.Sprintf(" (p. %d)", item.Locator.Start)
		}
		if item.Locator.Symbol != "" {
			locStr += " " + item.Locator.Symbol
		}

		armStr := ""
		if len(item.Arms) > 0 {
			armStr = fmt.Sprintf(" (%s, rank %d)", strings.Join(item.Arms, "+"), item.Rank)
		}

		line := fmt.Sprintf("[%d] %s%s%s", i+1, format.DisplayPath(item.Path), locStr, armStr)

		if i == p.Cursor {
			rows = append(rows, theme.PickerSelected.Render("> "+line))
		} else {
			rows = append(rows, theme.PickerNormal.Render("  "+line))
		}
	}

	hint := lipgloss.JoinHorizontal(
		lipgloss.Left,
		theme.KeyHintBracket.Render("["),
		theme.KeyHintKey.Render("↑/↓"),
		theme.KeyHintBracket.Render("]"),
		" navigate   ",
		theme.KeyHintBracket.Render("["),
		theme.KeyHintKey.Render("enter"),
		theme.KeyHintBracket.Render("]"),
		" select   ",
		theme.KeyHintBracket.Render("["),
		theme.KeyHintKey.Render("1-9"),
		theme.KeyHintBracket.Render("]"),
		" quick pick   ",
		theme.KeyHintBracket.Render("["),
		theme.KeyHintKey.Render("esc"),
		theme.KeyHintBracket.Render("]"),
		" cancel",
	)

	rows = append(rows, "", theme.KeyHintDesc.Render(hint))

	content := strings.Join(rows, "\n")
	boxWidth := width - 4
	if boxWidth < 40 {
		boxWidth = 40
	}

	return theme.PickerBox.Width(boxWidth).Render(content)
}
