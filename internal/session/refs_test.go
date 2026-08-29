package session

import (
	"testing"

	"github.com/Hendrixx-RE/Vektix/internal/store"
)

func TestSessionRefs_Resolution(t *testing.T) {
	s := NewStore()

	items := []Item{
		{Rank: 1, Path: "/path/to/main.go", Content: "package main"},
		{Rank: 2, Path: "/path/to/resume.pdf", Content: "Resume text"},
		{Rank: 3, Path: "/path/to/config.yaml", Content: "server: 8080"},
		{Rank: 4, Path: "/path/to/notes.md", Content: "# Notes"},
	}
	s.Set("query", items)

	if s.Count() != 4 {
		t.Fatalf("expected count 4, got %d", s.Count())
	}

	tests := []struct {
		ref       string
		wantIndex int
		wantPath  string
	}{
		// Pronouns
		{"it", 0, "/path/to/main.go"},
		{"that", 0, "/path/to/main.go"},
		{"this", 0, "/path/to/main.go"},
		{"the file", 0, "/path/to/main.go"},
		// Word ordinals
		{"the first one", 0, "/path/to/main.go"},
		{"first", 0, "/path/to/main.go"},
		{"the second one", 1, "/path/to/resume.pdf"},
		{"second", 1, "/path/to/resume.pdf"},
		{"the third one", 2, "/path/to/config.yaml"},
		{"third", 2, "/path/to/config.yaml"},
		{"the fourth one", 3, "/path/to/notes.md"},
		{"last", 3, "/path/to/notes.md"},
		{"the last one", 3, "/path/to/notes.md"},
		// Numeric ordinals
		{"#1", 0, "/path/to/main.go"},
		{"#2", 1, "/path/to/resume.pdf"},
		{"#3", 2, "/path/to/config.yaml"},
		{"#4", 3, "/path/to/notes.md"},
		{"1st", 0, "/path/to/main.go"},
		{"2nd", 1, "/path/to/resume.pdf"},
		{"3rd", 2, "/path/to/config.yaml"},
		{"4th", 3, "/path/to/notes.md"},
		{"1", 0, "/path/to/main.go"},
		{"2", 1, "/path/to/resume.pdf"},
		// Extension / type qualifiers
		{"that pdf", 1, "/path/to/resume.pdf"},
		{"the pdf", 1, "/path/to/resume.pdf"},
		{"the go file", 0, "/path/to/main.go"},
		{"the yaml file", 2, "/path/to/config.yaml"},
		{"that markdown", 3, "/path/to/notes.md"},
		// Substring
		{"resume", 1, "/path/to/resume.pdf"},
		{"config", 2, "/path/to/config.yaml"},
	}

	for _, tc := range tests {
		t.Run(tc.ref, func(t *testing.T) {
			item, idx, ok := s.ResolveRef(tc.ref)
			if !ok {
				t.Fatalf("failed to resolve %q", tc.ref)
			}
			if idx != tc.wantIndex {
				t.Errorf("ref %q: expected index %d, got %d", tc.ref, tc.wantIndex, idx)
			}
			if item.Path != tc.wantPath {
				t.Errorf("ref %q: expected path %q, got %q", tc.ref, tc.wantPath, item.Path)
			}
		})
	}

	// Out of bounds
	if _, _, ok := s.ResolveRef("#5"); ok {
		t.Errorf("expected #5 to fail on 4-item list")
	}
	if _, _, ok := s.ResolveRef("the fifth one"); ok {
		t.Errorf("expected fifth to fail on 4-item list")
	}

	// Clear invalidates
	s.Clear()
	if s.Count() != 0 {
		t.Errorf("expected count 0 after clear, got %d", s.Count())
	}
	if _, _, ok := s.ResolveRef("it"); ok {
		t.Errorf("expected resolution to fail after Clear()")
	}
}

func TestSessionRefs_GetAndSummary(t *testing.T) {
	s := NewStore()
	if s.FormatOrdinalSummary() != "" {
		t.Errorf("expected empty summary on empty store")
	}

	s.Set("query", []Item{
		{Rank: 1, Path: "/a/b.txt", Locator: store.Locator{Kind: store.LocatorLineRange, Start: 1, End: 10}},
	})
	if s.Count() != 1 {
		t.Fatalf("expected count 1, got %d", s.Count())
	}
	if it, ok := s.Get(0); !ok || it.Path != "/a/b.txt" {
		t.Errorf("unexpected Get(0) result: %+v, %v", it, ok)
	}
	if _, ok := s.Get(1); ok {
		t.Errorf("expected Get(1) to fail")
	}
}
