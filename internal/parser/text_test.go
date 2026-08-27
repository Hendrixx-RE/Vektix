package parser

import (
	"strings"
	"testing"
)

func TestParseText(t *testing.T) {
	input := "line 1\nline 2\r\nline 3\n"
	doc, err := ParseText(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseText failed: %v", err)
	}

	if len(doc.Lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(doc.Lines))
	}

	if doc.Lines[0].Number != 1 || doc.Lines[0].Content != "line 1" {
		t.Errorf("unexpected first line: %+v", doc.Lines[0])
	}
	if doc.Lines[1].Number != 2 || doc.Lines[1].Content != "line 2" {
		t.Errorf("unexpected second line: %+v", doc.Lines[1])
	}
	if doc.Lines[2].Number != 3 || doc.Lines[2].Content != "line 3" {
		t.Errorf("unexpected third line: %+v", doc.Lines[2])
	}
}
