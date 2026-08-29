package tui

import "github.com/charmbracelet/lipgloss"

// Theme holds the shared Lip Gloss styles for the Vektix TUI.
type Theme struct {
	Primary   lipgloss.Color
	Secondary lipgloss.Color
	Success   lipgloss.Color
	Warning   lipgloss.Color
	Error     lipgloss.Color
	Muted     lipgloss.Color
	Border    lipgloss.Color
	Highlight lipgloss.Color
	Text      lipgloss.Color
	Subtle    lipgloss.Color

	// Styles
	Title          lipgloss.Style
	ScopeBadge     lipgloss.Style
	ScopeGlobal    lipgloss.Style
	StatusBar      lipgloss.Style
	KeyHintKey     lipgloss.Style
	KeyHintBracket lipgloss.Style
	KeyHintDesc    lipgloss.Style

	Prompt         lipgloss.Style
	UserInput      lipgloss.Style
	UserQueryEcho  lipgloss.Style

	PathHeader     lipgloss.Style
	LineRange      lipgloss.Style
	Symbol         lipgloss.Style
	RankInfo       lipgloss.Style
	Gutter         lipgloss.Style
	ExcerptBorder  lipgloss.Style

	ActionBar      lipgloss.Style
	ActionKey      lipgloss.Style
	ActionLabel    lipgloss.Style
	ActionMore     lipgloss.Style

	SuccessText    lipgloss.Style
	WarningText    lipgloss.Style
	ErrorText      lipgloss.Style
	InfoText       lipgloss.Style

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

// DefaultTheme returns the standard Vektix dark/modern theme.
func DefaultTheme() Theme {
	t := Theme{
		Primary:   lipgloss.Color("#7D56F4"),
		Secondary: lipgloss.Color("#00D7D7"),
		Success:   lipgloss.Color("#04B575"),
		Warning:   lipgloss.Color("#FFB86C"),
		Error:     lipgloss.Color("#FF5F87"),
		Muted:     lipgloss.Color("#626262"),
		Border:    lipgloss.Color("#3A3A3A"),
		Highlight: lipgloss.Color("#24283B"),
		Text:      lipgloss.Color("#FAFAFA"),
		Subtle:    lipgloss.Color("#A0A0A0"),
	}

	t.Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(t.Secondary)

	t.ScopeBadge = lipgloss.NewStyle().
		Foreground(t.Secondary).
		Background(lipgloss.Color("#1A202C")).
		Padding(0, 1)

	t.ScopeGlobal = lipgloss.NewStyle().
		Foreground(t.Warning).
		Background(lipgloss.Color("#1A202C")).
		Padding(0, 1)

	t.StatusBar = lipgloss.NewStyle().
		Foreground(t.Subtle).
		Background(lipgloss.Color("#16161E")).
		Padding(0, 1)

	t.KeyHintKey = lipgloss.NewStyle().
		Bold(true).
		Foreground(t.Secondary)

	t.KeyHintBracket = lipgloss.NewStyle().
		Foreground(t.Muted)

	t.KeyHintDesc = lipgloss.NewStyle().
		Foreground(t.Subtle)

	t.Prompt = lipgloss.NewStyle().
		Bold(true).
		Foreground(t.Secondary)

	t.UserInput = lipgloss.NewStyle().
		Foreground(t.Text)

	t.UserQueryEcho = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#E2E8F0"))

	t.PathHeader = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7AA2F7"))

	t.LineRange = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#89DDFF"))

	t.Symbol = lipgloss.NewStyle().
		Foreground(t.Warning)

	t.RankInfo = lipgloss.NewStyle().
		Foreground(t.Muted).
		Italic(true)

	t.Gutter = lipgloss.NewStyle().
		Foreground(t.Muted)

	t.ExcerptBorder = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Border).
		Padding(0, 1)

	t.ActionBar = lipgloss.NewStyle().
		Foreground(t.Subtle).
		Padding(0, 0, 0, 1)

	t.ActionKey = lipgloss.NewStyle().
		Bold(true).
		Foreground(t.Secondary)

	t.ActionLabel = lipgloss.NewStyle().
		Foreground(t.Text)

	t.ActionMore = lipgloss.NewStyle().
		Foreground(t.Muted)

	t.SuccessText = lipgloss.NewStyle().
		Bold(true).
		Foreground(t.Success)

	t.WarningText = lipgloss.NewStyle().
		Foreground(t.Warning)

	t.ErrorText = lipgloss.NewStyle().
		Bold(true).
		Foreground(t.Error)

	t.InfoText = lipgloss.NewStyle().
		Foreground(t.Secondary)

	t.PickerBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Primary).
		Padding(1, 2)

	t.PickerTitle = lipgloss.NewStyle().
		Bold(true).
		Foreground(t.Secondary).
		MarginBottom(1)

	t.PickerSelected = lipgloss.NewStyle().
		Bold(true).
		Foreground(t.Text).
		Background(t.Highlight).
		Padding(0, 1)

	t.PickerNormal = lipgloss.NewStyle().
		Foreground(t.Subtle).
		Padding(0, 1)

	t.PickerRank = lipgloss.NewStyle().
		Foreground(t.Muted)

	t.IndexTitle = lipgloss.NewStyle().
		Bold(true).
		Foreground(t.Secondary)

	t.IndexStatLabel = lipgloss.NewStyle().
		Foreground(t.Subtle)

	t.IndexStatValue = lipgloss.NewStyle().
		Bold(true).
		Foreground(t.Text)

	t.ProgressBar = lipgloss.NewStyle().
		Foreground(t.Primary)

	t.ExplainHeader = lipgloss.NewStyle().
		Bold(true).
		Foreground(t.Warning)

	t.ExplainContent = lipgloss.NewStyle().
		Foreground(t.Text)

	return t
}

// Global default styles instance
var styles = DefaultTheme()
