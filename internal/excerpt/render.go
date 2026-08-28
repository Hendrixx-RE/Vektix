package excerpt

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/Hendrixx-RE/Vektix/internal/store"
)

// RenderConfig holds options for rendering.
type RenderConfig struct {
	HeaderRankInfo string // e.g., "(bm25+vec, rank 1)"
	// NoColor suppresses the ANSI highlight around the matched span. Callers that
	// emit machine-readable output (--json), write to the clipboard, or render to a
	// non-terminal must set it; escape codes there are corruption, not decoration.
	NoColor bool
}

// Render formats the expanded text with a header, line numbers, a gutter, and highlights the matched span.
func Render(chunk store.Chunk, expandedText string, loc store.Locator, cfg RenderConfig) string {
	var buf bytes.Buffer

	// 1. Render Header
	var headerLeft string
	if loc.Kind == store.LocatorPage {
		headerLeft = fmt.Sprintf("%s:page %d", chunk.Path, loc.Start)
	} else {
		if loc.Start == loc.End {
			headerLeft = fmt.Sprintf("%s:%d", chunk.Path, loc.Start)
		} else {
			headerLeft = fmt.Sprintf("%s:%d-%d", chunk.Path, loc.Start, loc.End)
		}
	}

	// Calculate spacing for right-aligned rank info.
	// We want the total width to be around 80 chars, matching the example.
	// Example: "~/notes/infra.md:41-47                                    (bm25+vec, rank 1)"
	targetWidth := 80
	padding := targetWidth - len(headerLeft) - len(cfg.HeaderRankInfo)
	if padding < 1 {
		padding = 1
	}
	
	header := headerLeft + strings.Repeat(" ", padding) + cfg.HeaderRankInfo
	buf.WriteString(header)
	buf.WriteByte('\n')

	// 2. Render Body
	matchStart := strings.Index(expandedText, chunk.Content)
	matchEnd := matchStart + len(chunk.Content)
	if matchStart == -1 {
		// Fallback if not found (shouldn't happen in normal flow)
		matchStart = -1
		matchEnd = -1
	}

	highlightOn, highlightOff := "\x1b[33m", "\x1b[0m" // yellow
	if cfg.NoColor {
		highlightOn, highlightOff = "", ""
	}

	lines := strings.Split(expandedText, "\n")
	
	// Determine gutter width based on max line number
	maxLineNum := loc.Start + len(lines) - 1
	gutterWidth := len(strconv.Itoa(maxLineNum))
	if gutterWidth < 4 {
		gutterWidth = 4 // Minimum gutter width to match example nicely
	}

	currentByteOffset := 0
	for i, line := range lines {
		lineNum := loc.Start + i
		
		// For Page locators, we do not have absolute line numbers, so we omit them from the gutter.
		var gutter string
		if loc.Kind == store.LocatorPage {
			gutter = fmt.Sprintf(" %*s | ", gutterWidth, "")
		} else {
			gutter = fmt.Sprintf(" %*d | ", gutterWidth, lineNum)
		}
		
		buf.WriteString(gutter)

		lineStart := currentByteOffset
		lineEnd := currentByteOffset + len(line)
		
		// Check intersection with match
		if matchStart != -1 && matchEnd > lineStart && matchStart < lineEnd {
			// There is an intersection
			intersectStart := matchStart
			if intersectStart < lineStart {
				intersectStart = lineStart
			}
			intersectEnd := matchEnd
			if intersectEnd > lineEnd {
				intersectEnd = lineEnd
			}

			// Indices relative to the line
			relStart := intersectStart - lineStart
			relEnd := intersectEnd - lineStart

			buf.WriteString(line[:relStart])
			buf.WriteString(highlightOn)
			buf.WriteString(line[relStart:relEnd])
			buf.WriteString(highlightOff)
			buf.WriteString(line[relEnd:])
		} else {
			// No intersection
			buf.WriteString(line)
		}

		buf.WriteByte('\n')
		currentByteOffset += len(line) + 1 // +1 for the newline
	}

	return buf.String()
}
