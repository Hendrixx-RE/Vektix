package tui

import "github.com/charmbracelet/lipgloss"

// Theme holds the shared Lip Gloss styles for the Vektix TUI. It intentionally sets no
// explicit colors: every style relies on the terminal's own default foreground/background
// and its own ANSI palette (via Bold/Italic/Faint/Reverse) so Vektix always matches
// whatever theme the user's terminal is already configured with.
type Theme struct {
	// Styles
	Title          lipgloss.Style
	ScopeBadge     lipgloss.Style
	ScopeGlobal    lipgloss.Style
	StatusBar      lipgloss.Style
	KeyHintKey     lipgloss.Style
	KeyHintBracket lipgloss.Style
	KeyHintDesc    lipgloss.Style

	Prompt        lipgloss.Style
	UserInput     lipgloss.Style
	UserQueryEcho lipgloss.Style

	PathHeader    lipgloss.Style
	LineRange     lipgloss.Style
	Symbol        lipgloss.Style
	RankInfo      lipgloss.Style
	Gutter        lipgloss.Style
	ExcerptBorder lipgloss.Style

	ActionBar   lipgloss.Style
	ActionKey   lipgloss.Style
	ActionLabel lipgloss.Style
	ActionMore  lipgloss.Style

	SuccessText lipgloss.Style
	WarningText lipgloss.Style
	ErrorText   lipgloss.Style
	InfoText    lipgloss.Style

	PickerBox      lipgloss.Style
	PickerTitle    lipgloss.Style
	PickerSelected lipgloss.Style
	PickerNormal   lipgloss.Style
	PickerRank     lipgloss.Style

	IndexTitle     lipgloss.Style
	IndexStatLabel lipgloss.Style
	IndexStatValue lipgloss.Style
	ProgressBar    lipgloss.Style

	ExplainHeader  lipgloss.Style
	ExplainContent lipgloss.Style
}

// DefaultTheme returns a theme that uses the terminal's own default colors throughout.
// Emphasis is conveyed with Bold, Italic, Faint (dim), and Reverse (swap fg/bg using the
// terminal's own palette) instead of any hardcoded color values.
func DefaultTheme() Theme {
	var t Theme

	t.Title = lipgloss.NewStyle().Bold(true)

	t.ScopeBadge = lipgloss.NewStyle().Reverse(true).Padding(0, 1)
	t.ScopeGlobal = lipgloss.NewStyle().Reverse(true).Bold(true).Padding(0, 1)
	t.StatusBar = lipgloss.NewStyle().Faint(true).Padding(0, 1)

	t.KeyHintKey = lipgloss.NewStyle().Bold(true)
	t.KeyHintBracket = lipgloss.NewStyle().Faint(true)
	t.KeyHintDesc = lipgloss.NewStyle().Faint(true)

	t.Prompt = lipgloss.NewStyle().Bold(true)
	t.UserInput = lipgloss.NewStyle()
	t.UserQueryEcho = lipgloss.NewStyle().Bold(true)

	t.PathHeader = lipgloss.NewStyle().Bold(true).Underline(true)
	t.LineRange = lipgloss.NewStyle().Faint(true)
	t.Symbol = lipgloss.NewStyle()
	t.RankInfo = lipgloss.NewStyle().Faint(true).Italic(true)
	t.Gutter = lipgloss.NewStyle().Faint(true)

	t.ExcerptBorder = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		Padding(0, 1)

	t.ActionBar = lipgloss.NewStyle().Faint(true).Padding(0, 0, 0, 1)
	t.ActionKey = lipgloss.NewStyle().Bold(true)
	t.ActionLabel = lipgloss.NewStyle()
	t.ActionMore = lipgloss.NewStyle().Faint(true)

	t.SuccessText = lipgloss.NewStyle().Bold(true)
	t.WarningText = lipgloss.NewStyle().Italic(true)
	t.ErrorText = lipgloss.NewStyle().Bold(true).Underline(true)
	t.InfoText = lipgloss.NewStyle()

	t.PickerBox = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		Padding(1, 2)

	t.PickerTitle = lipgloss.NewStyle().Bold(true).MarginBottom(1)
	t.PickerSelected = lipgloss.NewStyle().Reverse(true).Bold(true).Padding(0, 1)
	t.PickerNormal = lipgloss.NewStyle().Padding(0, 1)
	t.PickerRank = lipgloss.NewStyle().Faint(true)

	t.IndexTitle = lipgloss.NewStyle().Bold(true)
	t.IndexStatLabel = lipgloss.NewStyle().Faint(true)
	t.IndexStatValue = lipgloss.NewStyle().Bold(true)
	t.ProgressBar = lipgloss.NewStyle()

	t.ExplainHeader = lipgloss.NewStyle().Bold(true)
	t.ExplainContent = lipgloss.NewStyle()

	return t
}

// Global default styles instance
var styles = DefaultTheme()
