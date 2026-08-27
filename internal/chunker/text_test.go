package chunker

import (
	"strings"
	"testing"
)

func TestChunkText(t *testing.T) {
	content := `
# Header 1
This is a short sentence. This is another sentence.

## Header 2
A paragraph with some text here.
It spans multiple lines.

Another paragraph here.`

	chunks := ChunkText("test.md", content)
	if len(chunks) == 0 {
		t.Fatalf("expected chunks, got 0")
	}
	for _, c := range chunks {
		if c.Locator.Start == 0 || c.Locator.End == 0 {
			t.Errorf("invalid locator: %+v", c.Locator)
		}
	}
}

func TestChunkTextOversized(t *testing.T) {
	// A single word repeated, no spaces, to force splitOversized (actually splitOversized splits by space)
	// If no spaces, it won't split, but that's fine, it will just exceed maxTokens. The loop checks chunkTokens > 0 before breaking.
	content := strings.Repeat("word ", 1000)
	chunks := ChunkText("test.txt", content)
	if len(chunks) == 0 {
		t.Fatalf("expected chunks")
	}
	for _, c := range chunks {
		if len(c.Content) > 256*5 {
			t.Errorf("chunk too large")
		}
	}
}
