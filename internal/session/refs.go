package session

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Hendrixx-RE/Vektix/internal/store"
)

// Item represents a single search result tracked in the session.
type Item struct {
	Rank     int
	Path     string
	Locator  store.Locator
	Content  string // expanded text or chunk content
	Score    float64
	Arms     []string
	BestRank int
	Chunk    *store.Chunk
	Query    string
}

// Store tracks search results across queries in the session to support
// natural-language ordinal and pronoun references ("open the first one",
// "copy that", "#2", "that pdf").
type Store struct {
	items     []Item
	lastQuery string
}

// NewStore initializes a new session store.
func NewStore() *Store {
	return &Store{
		items: make([]Item, 0),
	}
}

// Set updates the stored results with a new query's result list.
func (s *Store) Set(query string, items []Item) {
	s.lastQuery = query
	s.items = make([]Item, len(items))
	copy(s.items, items)
}

// Items returns a copy of the current result items.
func (s *Store) Items() []Item {
	out := make([]Item, len(s.items))
	copy(out, s.items)
	return out
}

// Count returns the number of results currently tracked.
func (s *Store) Count() int {
	return len(s.items)
}

// LastQuery returns the query that produced the current results.
func (s *Store) LastQuery() string {
	return s.lastQuery
}

// Get returns the item at 0-indexed position idx.
func (s *Store) Get(idx int) (*Item, bool) {
	if idx < 0 || idx >= len(s.items) {
		return nil, false
	}
	item := s.items[idx]
	return &item, true
}

// Clear invalidates all session refs. per plan.md, changing scope
// MUST invalidate session ordinal refs so "open the first one" never points
// into a stale, differently-scoped result set.
func (s *Store) Clear() {
	s.items = make([]Item, 0)
	s.lastQuery = ""
}

var (
	hashNumberRe       = regexp.MustCompile(`^#?(\d+)(?:st|nd|rd|th)?$`)
	demonstrativeRefRe = regexp.MustCompile(`^(?:that|the|this)\s+([a-zA-Z0-9_.-]+)(?:\s+file|\s+code|\s+document)?$`)
)

var wordToOrdinal = map[string]int{
	"first":    0,
	"1st":      0,
	"second":   1,
	"2nd":      1,
	"third":    2,
	"3rd":      2,
	"fourth":   3,
	"4th":      3,
	"fifth":    4,
	"5th":      4,
	"sixth":    5,
	"6th":      5,
	"seventh":  6,
	"7th":      6,
	"eighth":   7,
	"8th":      7,
	"ninth":    8,
	"9th":      8,
	"tenth":    9,
	"10th":     9,
}

var explicitPronouns = map[string]bool{
	"it":          true,
	"that":        true,
	"this":        true,
	"the file":    true,
	"the match":   true,
	"the excerpt": true,
	"the result":  true,
	"selected":    true,
	"current":     true,
}

var extMap = map[string]string{
	"pdf":      ".pdf",
	"go":       ".go",
	"md":       ".md",
	"markdown": ".md",
	"txt":      ".txt",
	"text":     ".txt",
	"json":     ".json",
	"yaml":     ".yaml",
	"yml":      ".yml",
	"toml":     ".toml",
	"py":       ".py",
	"python":   ".py",
	"js":       ".js",
	"ts":       ".ts",
	"rs":       ".rs",
	"rust":     ".rs",
	"sh":       ".sh",
	"c":        ".c",
	"java":     ".java",
}

// IsExplicitRef tests whether a string is an explicit ordinal, pronoun, or
// demonstrative qualifier reference (e.g. "it", "#2", "the third one", "that pdf").
// Plain words like "resume" or "server" return false so they are never hijacked.
func IsExplicitRef(ref string) bool {
	norm := strings.ToLower(strings.TrimSpace(ref))
	norm = strings.Trim(norm, "\"'`.,;!?")
	if norm == "" {
		return false
	}

	if explicitPronouns[norm] {
		return true
	}

	if norm == "last" || norm == "the last" || norm == "the last one" || norm == "last one" {
		return true
	}

	cleanOrdinal := strings.TrimPrefix(norm, "the ")
	cleanOrdinal = strings.TrimSuffix(cleanOrdinal, " one")
	cleanOrdinal = strings.TrimSpace(cleanOrdinal)
	if _, ok := wordToOrdinal[cleanOrdinal]; ok {
		return true
	}

	if strings.HasPrefix(norm, "#") {
		return true
	}

	if m := hashNumberRe.FindStringSubmatch(cleanOrdinal); len(m) == 2 {
		if strings.HasSuffix(cleanOrdinal, "st") || strings.HasSuffix(cleanOrdinal, "nd") ||
			strings.HasSuffix(cleanOrdinal, "rd") || strings.HasSuffix(cleanOrdinal, "th") {
			return true
		}
	}

	if demonstrativeRefRe.MatchString(norm) {
		return true
	}

	return false
}

// ResolveRef attempts to resolve a reference string (such as "it", "that",
// "#2", "the third one", "that pdf", "last") to an Item in the session.
// It returns the resolved item, its 0-based index, and true if found.
func (s *Store) ResolveRef(ref string) (*Item, int, bool) {
	if len(s.items) == 0 {
		return nil, -1, false
	}

	norm := strings.ToLower(strings.TrimSpace(ref))
	norm = strings.Trim(norm, "\"'`.,;!?")
	if norm == "" {
		return nil, -1, false
	}

	// 1. Direct pronouns referring to the top/current result
	if explicitPronouns[norm] {
		item := s.items[0]
		return &item, 0, true
	}

	// 2. "the last one" / "last" / "last one"
	if norm == "last" || norm == "the last" || norm == "the last one" || norm == "last one" {
		idx := len(s.items) - 1
		item := s.items[idx]
		return &item, idx, true
	}

	// 3. Word ordinals: "the first one", "first", "the third", "second one"
	cleanOrdinal := strings.TrimPrefix(norm, "the ")
	cleanOrdinal = strings.TrimSuffix(cleanOrdinal, " one")
	cleanOrdinal = strings.TrimSpace(cleanOrdinal)
	if idx, ok := wordToOrdinal[cleanOrdinal]; ok {
		if idx < len(s.items) {
			item := s.items[idx]
			return &item, idx, true
		}
		return nil, -1, false
	}

	// 4. Numeric ordinals: "#1", "#2", "1", "2", "3rd", "4th"
	if m := hashNumberRe.FindStringSubmatch(cleanOrdinal); len(m) == 2 {
		if n, err := strconv.Atoi(m[1]); err == nil && n >= 1 {
			idx := n - 1
			if idx < len(s.items) {
				item := s.items[idx]
				return &item, idx, true
			}
			return nil, -1, false
		}
	}

	// 5. Explicit demonstrative file type qualifiers: "that pdf", "the go file", "that server.go"
	// Requires demonstrative prefix ("that", "the", "this") so bare queries like "resume" are never matched.
	if m := demonstrativeRefRe.FindStringSubmatch(norm); len(m) == 2 {
		token := m[1]
		if targetExt, ok := extMap[token]; ok {
			for i, it := range s.items {
				if strings.EqualFold(filepath.Ext(it.Path), targetExt) {
					item := it
					return &item, i, true
				}
			}
		}

		// Also check if token matches filename or basename directly with demonstrative prefix
		for i, it := range s.items {
			base := strings.ToLower(filepath.Base(it.Path))
			if strings.Contains(base, token) {
				item := it
				return &item, i, true
			}
		}
	}

	return nil, -1, false
}

// FormatOrdinalSummary returns a short helper string of available refs.
func (s *Store) FormatOrdinalSummary() string {
	if len(s.items) == 0 {
		return ""
	}
	if len(s.items) == 1 {
		return "1 match available"
	}
	return fmt.Sprintf("%d matches available (#1 to #%d)", len(s.items), len(s.items))
}
