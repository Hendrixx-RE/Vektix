package excerpt

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Hendrixx-RE/Vektix/internal/store"
)

// ExpandConfig holds options for expansion.
type ExpandConfig struct {
	MaxLines int
}

// Expand takes a chunk and the full text of its source (file or page)
// and returns the expanded text and the new locator.
func Expand(chunk store.Chunk, source []byte, cfg ExpandConfig) (string, store.Locator) {
	if len(source) == 0 {
		return chunk.Content, chunk.Locator
	}

	sourceStr := string(source)
	// We preserve the original lines exactly.
	lines := strings.Split(sourceStr, "\n")

	if chunk.Locator.Kind == store.LocatorPage {
		return expandPage(chunk, sourceStr, lines, cfg)
	}

	startLine := chunk.Locator.Start
	endLine := chunk.Locator.End

	if startLine < 1 {
		startLine = 1
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	if startLine > endLine {
		startLine = endLine
	}

	ext := strings.ToLower(filepath.Ext(chunk.Path))
	switch ext {
	case ".md", ".txt":
		startLine, endLine = expandProse(lines, startLine, endLine, cfg.MaxLines)
	case ".go":
		startLine, endLine = expandGo(source, lines, startLine, endLine, cfg.MaxLines)
	case ".json", ".yaml", ".yml", ".toml":
		startLine, endLine = expandStructured(lines, startLine, endLine, cfg.MaxLines)
	case ".py", ".js", ".ts", ".rs", ".sh", ".c", ".java", ".cpp", ".h", ".hpp":
		startLine, endLine = expandCode(lines, startLine, endLine, cfg.MaxLines)
	default:
		startLine, endLine = expandProse(lines, startLine, endLine, cfg.MaxLines)
	}

	newLoc := chunk.Locator
	newLoc.Start = startLine
	newLoc.End = endLine

	expandedText := strings.Join(lines[startLine-1:endLine], "\n")
	return expandedText, newLoc
}

func expandPage(chunk store.Chunk, sourceStr string, lines []string, cfg ExpandConfig) (string, store.Locator) {
	idx := strings.Index(sourceStr, chunk.Content)
	if idx == -1 {
		return chunk.Content, chunk.Locator
	}

	byteCount := 0
	startLine := 1
	for i, l := range lines {
		if byteCount+len(l)+1 > idx {
			startLine = i + 1
			break
		}
		byteCount += len(l) + 1
	}

	endIdx := idx + len(chunk.Content)
	byteCount = 0
	endLine := len(lines)
	for i, l := range lines {
		if byteCount+len(l)+1 >= endIdx {
			endLine = i + 1
			break
		}
		byteCount += len(l) + 1
	}

	s, e := expandProse(lines, startLine, endLine, cfg.MaxLines)
	loc := chunk.Locator
	// For page, we don't return line numbers in the locator.
	return strings.Join(lines[s-1:e], "\n"), loc
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func expandProse(lines []string, start, end, maxLines int) (int, int) {
	if end-start+1 >= maxLines {
		return start, min(end, start+maxLines-1)
	}

	for start > 1 && strings.TrimSpace(lines[start-2]) != "" {
		if end-start+2 > maxLines {
			break
		}
		start--
	}

	for end < len(lines) && strings.TrimSpace(lines[end]) != "" {
		if end-start+2 > maxLines {
			break
		}
		end++
	}
	return start, end
}

func expandStructured(lines []string, start, end, maxLines int) (int, int) {
	if end-start+1 >= maxLines {
		return start, min(end, start+maxLines-1)
	}

	isTopLevel := func(line string) bool {
		if len(line) == 0 {
			return false
		}
		if line[0] == ' ' || line[0] == '\t' || line[0] == '#' || line[0] == '/' {
			return false
		}
		return true
	}

	for start > 1 && !isTopLevel(lines[start-1]) {
		if end-start+2 > maxLines {
			break
		}
		start--
	}

	for end < len(lines) && !isTopLevel(lines[end]) {
		if end-start+2 > maxLines {
			break
		}
		end++
	}
	return start, end
}

func expandGo(source []byte, lines []string, start, end, maxLines int) (int, int) {
	if end-start+1 >= maxLines {
		return start, min(end, start+maxLines-1)
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", source, 0)
	if err != nil {
		return expandCode(lines, start, end, maxLines)
	}

	var bestNode ast.Node
	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			return true
		}

		pos := fset.Position(n.Pos()).Line
		endPos := fset.Position(n.End()).Line

		if pos <= start && endPos >= end {
			switch n.(type) {
			case *ast.FuncDecl, *ast.GenDecl:
				if bestNode == nil || (endPos-pos) < (fset.Position(bestNode.End()).Line-fset.Position(bestNode.Pos()).Line) {
					bestNode = n
				}
			}
			return true
		}
		return false
	})

	if bestNode == nil {
		return expandCode(lines, start, end, maxLines)
	}

	nodeStart := fset.Position(bestNode.Pos()).Line
	nodeEnd := fset.Position(bestNode.End()).Line

	return fitBudget(nodeStart, nodeEnd, start, end, maxLines)
}

func expandCode(lines []string, start, end, maxLines int) (int, int) {
	if end-start+1 >= maxLines {
		return start, min(end, start+maxLines-1)
	}

	declRe := regexp.MustCompile(`^(func|def|class|function|fn|type|impl|pub fn)\b`)

	nodeStart := start
	for nodeStart > 1 {
		if declRe.MatchString(lines[nodeStart-1]) {
			break
		}
		nodeStart--
	}
	if nodeStart == 1 && !declRe.MatchString(lines[0]) {
		nodeStart = start
	}

	nodeEnd := end
	for nodeEnd < len(lines) {
		if len(lines[nodeEnd]) > 0 && lines[nodeEnd][0] == '}' {
			nodeEnd++
			break
		}
		nodeEnd++
	}

	return fitBudget(nodeStart, nodeEnd, start, end, maxLines)
}

func fitBudget(nodeStart, nodeEnd, chunkStart, chunkEnd, maxLines int) (int, int) {
	if nodeEnd-nodeStart+1 <= maxLines {
		return nodeStart, nodeEnd
	}

	if chunkEnd-chunkStart+1 >= maxLines {
		return chunkStart, chunkStart + maxLines - 1
	}

	s := chunkStart
	e := chunkEnd
	for s > nodeStart && e-s+2 <= maxLines {
		s--
	}

	for e < nodeEnd && e-s+2 <= maxLines {
		e++
	}

	return s, e
}
