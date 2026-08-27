package store

import (
	"fmt"
	"strconv"
)

// LocatorKind defines the type of location a chunk references.
type LocatorKind string

const (
	LocatorLineRange LocatorKind = "LineRange"
	LocatorPage      LocatorKind = "Page"
	LocatorSymbol    LocatorKind = "Symbol"
)

// Locator pinpoints where a chunk came from in its source file.
type Locator struct {
	Kind   LocatorKind
	Start  int
	End    int
	Symbol string // For code chunks: the signature (e.g. "func (e *Engine) Index")
}

// Chunk represents a chunk of text stored in the vector database.
type Chunk struct {
	ID        string
	Path      string
	Content   string
	Embedding []float32
	Locator   Locator
}

// EncodeMetadata encodes the chunk's non-content fields into a map[string]string
// because chromem-go metadata is explicitly map[string]string.
func (c *Chunk) EncodeMetadata() map[string]string {
	return map[string]string{
		"path":          c.Path,
		"locator_kind":  string(c.Locator.Kind),
		"locator_start": strconv.Itoa(c.Locator.Start),
		"locator_end":   strconv.Itoa(c.Locator.End),
		"locator_sym":   c.Locator.Symbol,
	}
}

// DecodeMetadata populates a Chunk's fields from a chromem-go metadata map.
// It does not populate ID, Content, or Embedding, which come from other fields on the Result.
func DecodeMetadata(m map[string]string) (Chunk, error) {
	c := Chunk{
		Path: m["path"],
	}

	c.Locator.Kind = LocatorKind(m["locator_kind"])
	c.Locator.Symbol = m["locator_sym"]

	if startStr, ok := m["locator_start"]; ok && startStr != "" {
		start, err := strconv.Atoi(startStr)
		if err != nil {
			return Chunk{}, fmt.Errorf("invalid locator_start: %w", err)
		}
		c.Locator.Start = start
	}

	if endStr, ok := m["locator_end"]; ok && endStr != "" {
		end, err := strconv.Atoi(endStr)
		if err != nil {
			return Chunk{}, fmt.Errorf("invalid locator_end: %w", err)
		}
		c.Locator.End = end
	}

	return c, nil
}
