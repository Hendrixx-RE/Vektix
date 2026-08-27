package resolve

import (
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/Hendrixx-RE/Vektix/internal/store"
)

// BM25Index is a simple in-memory BM25 index over content and paths.
type BM25Index struct {
	k1            float64
	b             float64
	docCount      float64
	avgDocLength  float64
	docLengths    map[string]int
	postings      map[string]map[string]int // term -> docID -> freq
	chunks        map[string]store.Chunk
}

// NewBM25Index builds a BM25 index from a list of chunks.
func NewBM25Index(chunks []store.Chunk) *BM25Index {
	idx := &BM25Index{
		k1:         1.5,
		b:          0.75,
		docLengths: make(map[string]int),
		postings:   make(map[string]map[string]int),
		chunks:     make(map[string]store.Chunk),
	}

	var totalLen int
	for _, c := range chunks {
		idx.chunks[c.ID] = c
		// Index content AND paths
		text := c.Path + " " + c.Content
		terms := tokenize(text)
		idx.docLengths[c.ID] = len(terms)
		totalLen += len(terms)

		for _, term := range terms {
			if idx.postings[term] == nil {
				idx.postings[term] = make(map[string]int)
			}
			idx.postings[term][c.ID]++
		}
	}

	idx.docCount = float64(len(chunks))
	if idx.docCount > 0 {
		idx.avgDocLength = float64(totalLen) / idx.docCount
	}

	return idx
}

// Search returns BM25-ranked chunks, filtered by scope.
func (idx *BM25Index) Search(query string, scope string) ResultList {
	if query == "" {
		return nil
	}

	terms := tokenize(query)
	scores := make(map[string]float64)

	for _, term := range terms {
		if posting, ok := idx.postings[term]; ok {
			df := float64(len(posting))
			idf := math.Log(1 + (idx.docCount-df+0.5)/(df+0.5))

			for docID, tf := range posting {
				// Exact scope filtering
				chunk := idx.chunks[docID]
				if scope != "" && !strings.HasPrefix(chunk.Path, scope) {
					continue
				}

				dl := float64(idx.docLengths[docID])
				tfFloat := float64(tf)
				
				score := idf * (tfFloat * (idx.k1 + 1)) / (tfFloat + idx.k1*(1-idx.b+idx.b*(dl/idx.avgDocLength)))
				scores[docID] += score
			}
		}
	}

	type docScore struct {
		id    string
		score float64
	}
	var ranked []docScore
	for id, score := range scores {
		if score > 0 {
			ranked = append(ranked, docScore{id: id, score: score})
		}
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		return ranked[i].score > ranked[j].score
	})

	var results ResultList
	for _, rs := range ranked {
		results = append(results, idx.chunks[rs.id])
	}

	return results
}

func tokenize(text string) []string {
	text = strings.ToLower(text)
	f := func(c rune) bool {
		return unicode.IsSpace(c) || c == '/' || c == '\\' || c == '.' || c == ',' || c == ';' || c == ':' || c == '(' || c == ')' || c == '[' || c == ']' || c == '{' || c == '}' || c == '"' || c == '\'' || c == '`' || c == '!' || c == '?' || c == '<' || c == '>'
	}
	return strings.FieldsFunc(text, f)
}
