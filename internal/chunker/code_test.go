package chunker

import (
	"strings"
	"testing"

	"github.com/Hendrixx-RE/Vektix/internal/config"
)

func TestChunkCodeGo(t *testing.T) {
	content := `package main

import "fmt"

type Box[T any] struct {
	Value T
}

func (b *Box[T]) Get() T {
	return b.Value
}

func PrintHello() {
	fmt.Println("Hello")
}

// oversized func
func Oversized() {
` + strings.Repeat("\tfmt.Println(\"loooooong\")\n", 300) + `
}
`
	cfg := config.ChunkingConfig{MaxTokens: 256, OverlapTokens: 50, MinTokens: 20}
	chunks := ChunkCode("test.go", content, cfg)
	if len(chunks) == 0 {
		t.Fatalf("expected chunks")
	}

	foundBox := false
	foundGet := false
	foundPrint := false
	foundOversized := false
	
	for _, c := range chunks {
		if strings.Contains(c.Locator.Symbol, "type Box") {
			foundBox = true
		}
		if strings.Contains(c.Locator.Symbol, "func (b *Box[T]) Get") {
			foundGet = true
		}
		if strings.Contains(c.Locator.Symbol, "func PrintHello") {
			foundPrint = true
		}
		if strings.Contains(c.Locator.Symbol, "func Oversized") {
			foundOversized = true
			if !strings.HasPrefix(c.Content, "func Oversized") && !strings.Contains(c.Content, "... ") {
				t.Errorf("Oversized chunk missing signature prefix: %s", c.Content[:50])
			}
		}
	}
	
	if !foundBox { t.Errorf("missing Box type") }
	if !foundGet { t.Errorf("missing Get method") }
	if !foundPrint { t.Errorf("missing PrintHello func") }
	if !foundOversized { t.Errorf("missing Oversized func") }
}

func TestChunkCodeMalformed(t *testing.T) {
	// Must not panic
	content := `func ( {} ( ( invalid go code // !@#$`
	cfg := config.ChunkingConfig{MaxTokens: 256, OverlapTokens: 50, MinTokens: 20}
	chunks := ChunkCode("invalid.go", content, cfg)
	if len(chunks) == 0 {
		t.Fatalf("expected chunks even on malformed input")
	}
}

func TestChunkGenericCode(t *testing.T) {
	content := `
def my_function(x):
    return x * 2

class MyClass:
    def method(self):
        pass
`
	cfg := config.ChunkingConfig{MaxTokens: 256, OverlapTokens: 50, MinTokens: 20}
	chunks := ChunkCode("test.py", content, cfg)
	if len(chunks) == 0 {
		t.Fatalf("expected chunks")
	}
	
	foundDef := false
	foundClass := false
	for _, c := range chunks {
		if strings.Contains(c.Locator.Symbol, "def my_function") {
			foundDef = true
		}
		if strings.Contains(c.Locator.Symbol, "class MyClass") {
			foundClass = true
		}
	}
	if !foundDef { t.Errorf("missing def") }
	if !foundClass { t.Errorf("missing class") }
}
