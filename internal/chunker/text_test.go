package chunker

import (
	"strings"
	"testing"

	"github.com/Hendrixx-RE/Vektix/internal/config"
)

func TestChunkText(t *testing.T) {
	content := `
# Header 1
This is a short sentence. This is another sentence.

## Header 2
A paragraph with some text here.
It spans multiple lines.

Another paragraph here.`

	cfg := config.ChunkingConfig{MaxTokens: 256, OverlapTokens: 50, MinTokens: 20}
	chunks := ChunkText("test.md", content, cfg)
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
	cfg := config.ChunkingConfig{MaxTokens: 256, OverlapTokens: 50, MinTokens: 20}
	chunks := ChunkText("test.txt", content, cfg)
	if len(chunks) == 0 {
		t.Fatalf("expected chunks")
	}
	for _, c := range chunks {
		if len(c.Content) > 256*5 {
			t.Errorf("chunk too large")
		}
	}
}

func TestChunkTextConfigThreading(t *testing.T) {
	sentences := make([]string, 50)
	for i := range sentences {
		sentences[i] = "Sentence number " + strings.Repeat("a", 30) + ". "
	}
	content := strings.Join(sentences, "\n\n")

	cfgDefault := config.ChunkingConfig{MaxTokens: 256, OverlapTokens: 50, MinTokens: 20}
	chunksDefault := ChunkText("test.md", content, cfgDefault)

	cfgSmall := config.ChunkingConfig{MaxTokens: 60, OverlapTokens: 10, MinTokens: 5}
	chunksSmall := ChunkText("test.md", content, cfgSmall)

	if len(chunksSmall) <= len(chunksDefault) {
		t.Errorf("expected smaller MaxTokens to produce more chunks: got %d (small) vs %d (default)",
			len(chunksSmall), len(chunksDefault))
	}

	if len(chunksDefault) > 0 && len(chunksSmall) > 0 {
		if chunksDefault[0].Content == chunksSmall[0].Content {
			t.Errorf("expected different chunk content with different max_tokens")
		}
	}
}
