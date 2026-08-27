package excerpt

import (
	"testing"

	"github.com/Hendrixx-RE/Vektix/internal/store"
)

func TestExpandProse(t *testing.T) {
	text := `Title
First paragraph here.
Still first.

Second paragraph.
Here it is.

Third paragraph.`
	
	chunk := store.Chunk{
		Path:    "doc.md",
		Content: "Still first.",
		Locator: store.Locator{Kind: store.LocatorLineRange, Start: 3, End: 3},
	}

	expanded, newLoc := Expand(chunk, []byte(text), ExpandConfig{MaxLines: 10})
	expected := "Title\nFirst paragraph here.\nStill first."
	if expanded != expected {
		t.Errorf("expected %q, got %q", expected, expanded)
	}
	if newLoc.Start != 1 || newLoc.End != 3 {
		t.Errorf("expected lines 1-3, got %d-%d", newLoc.Start, newLoc.End)
	}
}

func TestExpandGo(t *testing.T) {
	text := `package main

import "fmt"

func foo() {
	fmt.Println("hello")
	// middle
	fmt.Println("world")
}

func bar() {
}
`
	chunk := store.Chunk{
		Path:    "main.go",
		Content: "\t// middle\n",
		Locator: store.Locator{Kind: store.LocatorLineRange, Start: 7, End: 7},
	}

	expanded, newLoc := Expand(chunk, []byte(text), ExpandConfig{MaxLines: 10})
	
	expected := `func foo() {
	fmt.Println("hello")
	// middle
	fmt.Println("world")
}`
	if expanded != expected {
		t.Errorf("expected %q, got %q", expected, expanded)
	}
	if newLoc.Start != 5 || newLoc.End != 9 {
		t.Errorf("expected lines 5-9, got %d-%d", newLoc.Start, newLoc.End)
	}
}

func TestExpandStructured(t *testing.T) {
	text := `key1:
  val: 1
key2:
  nested:
    - a
    - b
key3:
  val: 2`

	chunk := store.Chunk{
		Path:    "config.yaml",
		Content: "    - a\n    - b",
		Locator: store.Locator{Kind: store.LocatorLineRange, Start: 5, End: 6},
	}

	expanded, newLoc := Expand(chunk, []byte(text), ExpandConfig{MaxLines: 10})
	
	expected := `key2:
  nested:
    - a
    - b`
	if expanded != expected {
		t.Errorf("expected %q, got %q", expected, expanded)
	}
	if newLoc.Start != 3 || newLoc.End != 6 {
		t.Errorf("expected lines 3-6, got %d-%d", newLoc.Start, newLoc.End)
	}
}

func TestExpandCodeHeuristic(t *testing.T) {
	text := `
def my_func():
    a = 1
    b = 2
    return a + b
}
`
	chunk := store.Chunk{
		Path:    "test.py",
		Content: "    b = 2",
		Locator: store.Locator{Kind: store.LocatorLineRange, Start: 4, End: 4},
	}
	
	expanded, newLoc := Expand(chunk, []byte(text), ExpandConfig{MaxLines: 10})
	expected := `def my_func():
    a = 1
    b = 2
    return a + b
}`
	if expanded != expected {
		t.Errorf("expected %q, got %q", expected, expanded)
	}
	if newLoc.Start != 2 || newLoc.End != 6 {
		t.Errorf("expected lines 2-6, got %d-%d", newLoc.Start, newLoc.End)
	}
}

func TestExpandBudget(t *testing.T) {
	text := `func huge() {
	// 1
	// 2
	// 3
	// 4
	// 5
}`
	chunk := store.Chunk{
		Path:    "huge.go",
		Content: "\t// 4\n\t// 5\n",
		Locator: store.Locator{Kind: store.LocatorLineRange, Start: 5, End: 6},
	}
	
	expanded, _ := Expand(chunk, []byte(text), ExpandConfig{MaxLines: 4})
	expected := `	// 2
	// 3
	// 4
	// 5`
	if expanded != expected {
		t.Errorf("expected %q, got %q", expected, expanded)
	}
}
