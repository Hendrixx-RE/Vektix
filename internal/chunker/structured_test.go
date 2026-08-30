package chunker

import (
	"testing"

	"github.com/Hendrixx-RE/Vektix/internal/config"
)

func TestChunkStructuredJSON(t *testing.T) {
	content := `{
  "key1": "value1",
  "key2": {
    "nested": "value"
  }
}`
	cfg := config.ChunkingConfig{MaxTokens: 256, OverlapTokens: 50, MinTokens: 20}
	chunks := ChunkStructured("test.json", content, cfg)
	if len(chunks) == 0 {
		t.Fatalf("expected chunks")
	}
}

func TestChunkStructuredYAML(t *testing.T) {
	content := `
services:
  web:
    image: nginx
  db:
    image: postgres
`
	cfg := config.ChunkingConfig{MaxTokens: 256, OverlapTokens: 50, MinTokens: 20}
	chunks := ChunkStructured("test.yaml", content, cfg)
	if len(chunks) == 0 {
		t.Fatalf("expected chunks")
	}
}

func TestChunkStructuredTOML(t *testing.T) {
	content := `
[package]
name = "test"

[dependencies]
pkg = "1.0"
`
	cfg := config.ChunkingConfig{MaxTokens: 256, OverlapTokens: 50, MinTokens: 20}
	chunks := ChunkStructured("test.toml", content, cfg)
	if len(chunks) == 0 {
		t.Fatalf("expected chunks")
	}
}
