package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/Hendrixx-RE/Vektix/internal/excerpt"
	"github.com/Hendrixx-RE/Vektix/internal/router"
	"github.com/Hendrixx-RE/Vektix/internal/store"
	"github.com/charmbracelet/lipgloss"
)

// SearchResult represents a resolved match rendered in the chat stream.
type SearchResult struct {
	Chunk    store.Chunk
	Text     string
	Locator  store.Locator
	Score    float64
	Arms     []string
	BestRank int
	Rank     int
	ArmLabel string
}

// ChatEntry represents a conversation item (user query, system output, search result card, or explain).
type ChatEntry struct {
	IsUser         bool
	Query          string
	Intent         *router.Intent
	Results        []SearchResult
	ActiveIndex    int
	Notice         string
	SuccessMsg     string
	ErrorMsg       string
	WarningMsg     string
	ExplainContent string
	ExplainLoading bool
	ExplainModel   string
	Timestamp      time.Time
}

// RenderChat formats the full chat history for viewport display.
func RenderChat(entries []ChatEntry, width int, theme Theme) string {
	if len(entries) == 0 {
		return renderWelcomeMessage(width, theme)
	}

	var sections []string

	for _, entry := range entries {
		var block []string

		if entry.IsUser {
			// User query prompt: > where is my resume
			userLine := lipgloss.JoinHorizontal(
				lipgloss.Left,
				theme.Prompt.Render("> "),
				theme.UserQueryEcho.Render(entry.Query),
			)
			block = append(block, userLine)
		} else {
			// Notice / Info
			if entry.Notice != "" {
				block = append(block, theme.InfoText.Render(entry.Notice))
			}

			// Warning
			if entry.WarningMsg != "" {
				block = append(block, theme.WarningText.Render(entry.WarningMsg))
			}

			// Error
			if entry.ErrorMsg != "" {
				block = append(block, theme.ErrorText.Render(entry.ErrorMsg))
			}

			// Success (e.g. ✓ opened main.go:88 in nvim)
			if entry.SuccessMsg != "" {
				block = append(block, theme.SuccessText.Render(entry.SuccessMsg))
			}

			// Search Results / Excerpt
			if len(entry.Results) > 0 {
				idx := entry.ActiveIndex
				if idx < 0 || idx >= len(entry.Results) {
					idx = 0
				}
				res := entry.Results[idx]

				renderedExcerpt := excerpt.Render(res.Chunk, res.Text, res.Locator, excerpt.RenderConfig{
					HeaderRankInfo: res.ArmLabel,
					NoColor:        false,
				})
				block = append(block, renderedExcerpt)

				// Action bar: [o]pen  [c]opy  [e]xplain  [n]ext match (X more)
				actionBar := RenderActionBar(entry, theme)
				block = append(block, actionBar)
			}

			// Explain view
			if entry.ExplainLoading {
				modelName := entry.ExplainModel
				if modelName == "" {
					modelName = "qwen2.5:3b-instruct"
				}
				loading := fmt.Sprintf("⚡ Explaining with %s (loaded on demand)...", modelName)
				block = append(block, theme.WarningText.Render(loading))
			}

			if entry.ExplainContent != "" {
				explainBox := theme.ExcerptBorder.Width(width - 4).Render(
					lipgloss.JoinVertical(
						lipgloss.Left,
						theme.ExplainHeader.Render("📝 Explanation:"),
						"",
						theme.ExplainContent.Render(entry.ExplainContent),
					),
				)
				block = append(block, explainBox)
			}
		}

		if len(block) > 0 {
			sections = append(sections, strings.Join(block, "\n"))
		}
	}

	return strings.Join(sections, "\n\n")
}

// RenderActionBar renders the [o]pen [c]opy [e]xplain [n]ext keybind footer.
func RenderActionBar(entry ChatEntry, theme Theme) string {
	moreCount := len(entry.Results) - 1
	var moreLabel string
	if moreCount > 0 {
		moreLabel = fmt.Sprintf("  %d more match", moreCount)
		if moreCount > 1 {
			moreLabel += "es"
		}
	}

	btn := func(key, name string) string {
		return lipgloss.JoinHorizontal(
			lipgloss.Left,
			theme.KeyHintBracket.Render("["),
			theme.ActionKey.Render(key),
			theme.KeyHintBracket.Render("]"),
			theme.ActionLabel.Render(name),
		)
	}

	items := []string{
		btn("o", "pen"),
		btn("c", "opy"),
		btn("e", "xplain"),
		btn("n", "ext"),
	}

	bar := strings.Join(items, "  ")
	if moreLabel != "" {
		bar += theme.ActionMore.Render(moreLabel)
	}

	return theme.ActionBar.Render(bar)
}

func renderWelcomeMessage(width int, theme Theme) string {
	var lines []string
	lines = append(lines, theme.Title.Render("🔷 Vektix — Natural Language File Locator & Passage Retrieval"))
	lines = append(lines, theme.KeyHintDesc.Render("Ask in plain English, or use quick actions:"))
	lines = append(lines, "")
	lines = append(lines, "  • "+theme.UserQueryEcho.Render("where is my resume")+"           "+theme.KeyHintDesc.Render("(locate by name or content)"))
	lines = append(lines, "  • "+theme.UserQueryEcho.Render("what's my postgres connection")+"  "+theme.KeyHintDesc.Render("(excerpt matching passage)"))
	lines = append(lines, "  • "+theme.UserQueryEcho.Render("open main.go")+"                    "+theme.KeyHintDesc.Render("(open in your editor)"))
	lines = append(lines, "  • "+theme.UserQueryEcho.Render("copy that")+"                       "+theme.KeyHintDesc.Render("(copy current passage)"))
	lines = append(lines, "  • "+theme.UserQueryEcho.Render(":scope <path>")+"                   "+theme.KeyHintDesc.Render("(change active search scope)"))
	lines = append(lines, "  • "+theme.UserQueryEcho.Render(":sync")+"                           "+theme.KeyHintDesc.Render("(sync & purge orphan chunks)"))
	lines = append(lines, "")
	lines = append(lines, theme.KeyHintDesc.Render("Type a query below and press [enter] to search."))

	boxWidth := width - 4
	if boxWidth < 50 {
		boxWidth = 50
	}
	return theme.ExcerptBorder.Width(boxWidth).Render(strings.Join(lines, "\n"))
}
