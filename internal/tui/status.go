package tui

import (
	"fmt"
	"strings"

	"github.com/Hendrixx-RE/Vektix/internal/config"
	"github.com/Hendrixx-RE/Vektix/internal/format"
	"github.com/Hendrixx-RE/Vektix/internal/resolve"
	"github.com/charmbracelet/lipgloss"
)

// ScopeState encapsulates the active search scope and its index statistics.
type ScopeState struct {
	Path       string // "" means global
	Global     bool
	Chunks     int
	Total      int
	HasIndex   bool
	Unindexed  bool   // CWD is outside every indexed root
	IndexError string
}

// Name returns the user-facing display string for the scope ("global" or shortened path).
func (s ScopeState) Name() string {
	if s.Global || s.Path == "" {
		return "global"
	}
	return format.DisplayPath(s.Path)
}

// Describe returns the scope plus its chunk count (e.g. "~/projects/go/vektix (412 chunks)").
func (s ScopeState) Describe() string {
	if !s.HasIndex {
		return fmt.Sprintf("%s (no index)", s.Name())
	}
	return fmt.Sprintf("%s (%s chunks)", s.Name(), format.HumanInt(s.Chunks))
}

// Banner returns the single-line summary shown in status bars and messages.
func (s ScopeState) Banner() string {
	switch {
	case !s.HasIndex:
		return fmt.Sprintf("scope: %s — %s", s.Name(), s.IndexError)
	case s.Global:
		return fmt.Sprintf("scope: global (%s chunks)", format.HumanInt(s.Total))
	default:
		return fmt.Sprintf("scope: %s (%s of %s chunks)",
			s.Name(), format.HumanInt(s.Chunks), format.HumanInt(s.Total))
	}
}

// ResolveScopeState resolves the active scope using the config, cwd, and root hierarchy.
func ResolveScopeState(cfg *config.Config, cwd, scopeOverride string, global bool, totalChunks int, countFn func(scope string) int) ScopeState {
	st := ScopeState{
		Total:    totalChunks,
		HasIndex: totalChunks > 0,
	}

	roots := make([]string, 0, len(cfg.Index.IndexDirs))
	for _, r := range cfg.Index.IndexDirs {
		if exp, err := config.ExpandPath(r); err == nil {
			roots = append(roots, exp)
		}
	}

	res, err := resolve.ResolveScope(cwd, scopeOverride, global, cfg.General.ScopeMode, roots)
	if err != nil {
		st.Global = true
	} else if res.RequiresPrompt && scopeOverride == "" {
		st.Global = true
		st.Unindexed = true
		st.Path = ""
	} else {
		st.Path = res.Path
		st.Global = res.Path == ""
	}

	if countFn != nil {
		st.Chunks = countFn(st.Path)
	} else if st.Global {
		st.Chunks = totalChunks
	}

	return st
}

// RenderStatusBar renders the top status bar containing the logo, active scope, and global toggle hint.
func RenderStatusBar(width int, st ScopeState, theme Theme) string {
	logo := theme.Title.Render("🔷 VEKTIX")

	var scopeBadge string
	if !st.HasIndex {
		scopeBadge = theme.ErrorText.Render("no index")
	} else if st.Global || st.Path == "" {
		scopeBadge = theme.ScopeGlobal.Render(fmt.Sprintf("scope: global (%s)", format.HumanInt(st.Total)))
	} else {
		scopeBadge = theme.ScopeBadge.Render(fmt.Sprintf("scope: %s (%s)", st.Name(), format.HumanInt(st.Chunks)))
	}

	hint := lipgloss.JoinHorizontal(
		lipgloss.Left,
		theme.KeyHintBracket.Render("["),
		theme.KeyHintKey.Render("g"),
		theme.KeyHintBracket.Render("]"),
		" ",
		theme.KeyHintDesc.Render("global"),
		"   ",
		theme.KeyHintBracket.Render("["),
		theme.KeyHintKey.Render(":"),
		theme.KeyHintBracket.Render("]"),
		theme.KeyHintDesc.Render("cmd"),
	)

	left := lipgloss.JoinHorizontal(lipgloss.Center, logo, "  ", scopeBadge)
	right := hint

	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}

	barContent := lipgloss.JoinHorizontal(
		lipgloss.Center,
		left,
		strings.Repeat(" ", gap),
		right,
	)

	return theme.StatusBar.Width(width).Render(barContent)
}
