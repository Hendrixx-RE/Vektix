package chunker

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Hendrixx-RE/Vektix/internal/store"
)

var (
	jsonKeyRe = regexp.MustCompile(`^(?:\s*)"([^"]+)"\s*:`)
	yamlKeyRe = regexp.MustCompile(`^([a-zA-Z0-9_-]+)\s*:`)
	tomlKeyRe = regexp.MustCompile(`^(?:\[([a-zA-Z0-9_\.-]+)\]|([a-zA-Z0-9_-]+)\s*=)`)
)

func ChunkStructured(path, content string) []store.Chunk {
	ext := strings.ToLower(filepath.Ext(path))
	var keyRe *regexp.Regexp
	switch ext {
	case ".json":
		keyRe = jsonKeyRe
	case ".yaml", ".yml":
		keyRe = yamlKeyRe
	case ".toml":
		keyRe = tomlKeyRe
	}

	if keyRe == nil {
		return ChunkText(path, content)
	}

	var chunks []store.Chunk
	lines := strings.Split(content, "\n")
	
	var currentKey string
	var currentChunk strings.Builder
	startLine := 1

	flush := func(endLine int) {
		text := currentChunk.String()
		if len(strings.TrimSpace(text)) > 0 {
			prefix := ""
			if currentKey != "" {
				prefix = currentKey + "\n"
			}
			chunks = append(chunks, store.Chunk{
				Path:    path,
				Content: prefix + text,
				Locator: store.Locator{
					Kind:  store.LocatorLineRange,
					Start: startLine,
					End:   endLine,
				},
			})
		}
		currentChunk.Reset()
	}

	for i, line := range lines {
		// Only consider top-level keys. Indentation should be 0 or small.
		// For JSON, we might have { on line 1, and keys indented by 2 or 4 spaces.
		// Check if it matches the key regex, limiting leading spaces to capture top-level keys.
		leadingSpaces := len(line) - len(strings.TrimLeft(line, " \t"))
		if leadingSpaces <= 4 {
			matches := keyRe.FindStringSubmatch(line)
			if len(matches) > 0 {
				keyName := matches[1]
				if ext == ".toml" && len(matches) > 2 && matches[2] != "" {
					keyName = matches[2]
				}
				
				flush(i)
				startLine = i + 1
				currentKey = keyName
			}
		}
		
		currentChunk.WriteString(line + "\n")
	}
	flush(len(lines))
	
	return chunks
}
