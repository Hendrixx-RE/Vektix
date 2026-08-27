package resolve

import (
	"strings"

	"github.com/sahilm/fuzzy"
	"github.com/Hendrixx-RE/Vektix/internal/store"
)

// PathIndex is an in-memory index of file paths for fuzzy searching.
// It implements a conceptual trie / filtered list as specified.
type PathIndex struct {
	paths  []string
	chunks map[string][]store.Chunk
}

// NewPathIndex builds a PathIndex from a list of chunks.
func NewPathIndex(chunks []store.Chunk) *PathIndex {
	idx := &PathIndex{
		chunks: make(map[string][]store.Chunk),
	}
	
	uniquePaths := make(map[string]struct{})
	for _, c := range chunks {
		idx.chunks[c.Path] = append(idx.chunks[c.Path], c)
		if _, ok := uniquePaths[c.Path]; !ok {
			uniquePaths[c.Path] = struct{}{}
			idx.paths = append(idx.paths, c.Path)
		}
	}
	
	return idx
}

// Search returns fuzzy-matched chunks, filtered by scope.
func (idx *PathIndex) Search(query string, scope string) ResultList {
	if query == "" {
		return nil
	}

	// 1. Exact scope filter: only consider paths having the scope as a prefix.
	// This acts as the exact prefix match on the path tree.
	var scopedPaths []string
	for _, p := range idx.paths {
		if scope == "" || strings.HasPrefix(p, scope) {
			scopedPaths = append(scopedPaths, p)
		}
	}

	// 2. Fuzzy match against the filtered paths (covers basename, stem, dir, extension).
	matches := fuzzy.Find(query, scopedPaths)
	
	var results ResultList
	for _, m := range matches {
		matchedPath := m.Str
		// Return the first chunk of each matched file.
		if chunks, ok := idx.chunks[matchedPath]; ok && len(chunks) > 0 {
			results = append(results, chunks[0])
		}
	}
	
	return results
}
