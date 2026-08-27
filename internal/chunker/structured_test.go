package chunker

import (
	"testing"
)

func TestChunkStructuredJSON(t *testing.T) {
	content := `{
  "key1": "value1",
  "key2": {
    "nested": "value"
  }
}`
	chunks := ChunkStructured("test.json", content)
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
	chunks := ChunkStructured("test.yaml", content)
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
	chunks := ChunkStructured("test.toml", content)
	if len(chunks) == 0 {
		t.Fatalf("expected chunks")
	}
}
