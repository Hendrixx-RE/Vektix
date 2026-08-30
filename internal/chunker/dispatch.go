package chunker

import (
	"path/filepath"
	"strings"

	"github.com/Hendrixx-RE/Vektix/internal/config"
	"github.com/Hendrixx-RE/Vektix/internal/store"
)

// Chunk parses the given file content into manageable chunks based on file extension.
func Chunk(path, content string, cfg config.ChunkingConfig) []store.Chunk {
	ext := strings.ToLower(filepath.Ext(path))
	
	switch ext {
	case ".md", ".txt", ".pdf":
		return ChunkText(path, content, cfg)
	case ".go", ".py", ".js", ".ts", ".rs", ".sh", ".c", ".java":
		return ChunkCode(path, content, cfg)
	case ".json", ".yaml", ".yml", ".toml":
		return ChunkStructured(path, content, cfg)
	default:
		return ChunkText(path, content, cfg)
	}
}
