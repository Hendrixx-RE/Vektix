package parser

import (
	"bufio"
	"io"
	"strings"
)

// TextLine represents a line of text with its 1-based line number.
type TextLine struct {
	Number  int
	Content string
}

// TextDocument represents a parsed text or markdown document.
type TextDocument struct {
	Lines []TextLine
}

// ParseText extracts lines from plain text or markdown content,
// returning them with 1-based line numbers to support LineRange locators.
func ParseText(r io.Reader) (*TextDocument, error) {
	var lines []TextLine
	reader := bufio.NewReader(r)
	lineNumber := 1
	
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			content := strings.TrimRight(line, "\r\n")
			lines = append(lines, TextLine{
				Number:  lineNumber,
				Content: content,
			})
			lineNumber++
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
	}
	
	return &TextDocument{Lines: lines}, nil
}
