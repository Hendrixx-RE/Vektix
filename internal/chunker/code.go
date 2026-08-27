package chunker

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Hendrixx-RE/Vektix/internal/ollama"
	"github.com/Hendrixx-RE/Vektix/internal/store"
)

var codeDeclRe = regexp.MustCompile(`^(func|def|class|function|fn|type|impl|pub\s+fn)\s+([a-zA-Z0-9_:]+)`)

func ChunkCode(path, content string) []store.Chunk {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".go" {
		return chunkGo(path, content)
	}
	return chunkGenericCode(path, content)
}

func chunkGo(path, content string) []store.Chunk {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, content, parser.ParseComments)
	if err != nil {
		// Fallback to generic if parse fails
		return chunkGenericCode(path, content)
	}

	var chunks []store.Chunk
	lines := strings.Split(content, "\n")
	
	// We want to chunk top-level declarations (functions, types)
	// Other parts (package, imports, globals) can be grouped or windowed.
	
	// For simplicity, let's just find the start and end lines of all Decls.
	// We will iterate through Decls.
	
	lastEndLine := 1
	
	for _, decl := range f.Decls {
		startLine := fset.Position(decl.Pos()).Line
		endLine := fset.Position(decl.End()).Line
		
		// Any code before this decl (like package, imports) -> chunk text?
		if startLine > lastEndLine {
			prefixLines := lines[lastEndLine-1 : startLine-1]
			prefixText := strings.Join(prefixLines, "\n")
			if strings.TrimSpace(prefixText) != "" {
				// Windowed chunking for text
				textChunks := ChunkText(path, prefixText)
				// Adjust line numbers
				for i := range textChunks {
					textChunks[i].Locator.Start += lastEndLine - 1
					textChunks[i].Locator.End += lastEndLine - 1
				}
				chunks = append(chunks, textChunks...)
			}
		}
		
		declLines := lines[startLine-1 : endLine]
		declText := strings.Join(declLines, "\n")
		
		var sym string
		switch d := decl.(type) {
		case *ast.FuncDecl:
			sym = "func "
			if d.Recv != nil && len(d.Recv.List) > 0 {
				sym += "("
				// just grab the string representation of receiver type
				// Actually, easier to just extract the first line as signature
				sigLine := declLines[0]
				// remove { if present
				if idx := strings.Index(sigLine, "{"); idx != -1 {
					sigLine = sigLine[:idx]
				}
				sym = strings.TrimSpace(sigLine)
			} else {
				sigLine := declLines[0]
				if idx := strings.Index(sigLine, "{"); idx != -1 {
					sigLine = sigLine[:idx]
				}
				sym = strings.TrimSpace(sigLine)
			}
		case *ast.GenDecl:
			if d.Tok == token.TYPE {
				sym = "type"
				if len(d.Specs) > 0 {
					if ts, ok := d.Specs[0].(*ast.TypeSpec); ok {
						sym = "type " + ts.Name.Name
					}
				}
			}
		}
		
		// If oversized, window it but retain signature
		if ollama.EstimateTokens(declText) > maxTokens {
			subChunks := windowOversizedCode(path, declText, startLine, sym)
			chunks = append(chunks, subChunks...)
		} else {
			chunks = append(chunks, store.Chunk{
				Path:    path,
				Content: declText,
				Locator: store.Locator{
					Kind:   store.LocatorSymbol,
					Start:  startLine,
					End:    endLine,
					Symbol: sym,
				},
			})
		}
		
		lastEndLine = endLine + 1
	}
	
	// remaining
	if lastEndLine <= len(lines) {
		remLines := lines[lastEndLine-1:]
		remText := strings.Join(remLines, "\n")
		if strings.TrimSpace(remText) != "" {
			textChunks := ChunkText(path, remText)
			for i := range textChunks {
				textChunks[i].Locator.Start += lastEndLine - 1
				textChunks[i].Locator.End += lastEndLine - 1
			}
			chunks = append(chunks, textChunks...)
		}
	}

	return chunks
}

func chunkGenericCode(path, content string) []store.Chunk {
	var chunks []store.Chunk
	lines := strings.Split(content, "\n")
	
	var currentSym string
	var currentChunk strings.Builder
	startLine := 1
	
	flush := func(endLine int) {
		text := currentChunk.String()
		if len(strings.TrimSpace(text)) > 0 {
			if ollama.EstimateTokens(text) > maxTokens {
				subChunks := windowOversizedCode(path, text, startLine, currentSym)
				chunks = append(chunks, subChunks...)
			} else {
				kind := store.LocatorLineRange
				if currentSym != "" {
					kind = store.LocatorSymbol
				}
				chunks = append(chunks, store.Chunk{
					Path:    path,
					Content: text,
					Locator: store.Locator{
						Kind:   kind,
						Start:  startLine,
						End:    endLine,
						Symbol: currentSym,
					},
				})
			}
		}
		currentChunk.Reset()
	}
	
	for i, line := range lines {
		if codeDeclRe.MatchString(line) {
			flush(i)
			startLine = i + 1
			
			// Extract signature (just the line up to { or :)
			sym := line
			if idx := strings.IndexAny(sym, "{:"); idx != -1 {
				sym = sym[:idx]
			}
			currentSym = strings.TrimSpace(sym)
		}
		currentChunk.WriteString(line + "\n")
	}
	flush(len(lines))
	
	return chunks
}

func windowOversizedCode(path, text string, startLineOffset int, sym string) []store.Chunk {
	var chunks []store.Chunk
	
	// We use ChunkText to window it, then adjust locators and add signature.
	textChunks := ChunkText(path, text)
	
	for _, tc := range textChunks {
		content := tc.Content
		if sym != "" {
			// Retain the signature in every chunk so each stays self-describing.
			if !strings.HasPrefix(strings.TrimSpace(content), sym) {
				content = sym + "\n... " + content
			}
		}
		
		chunks = append(chunks, store.Chunk{
			Path:    path,
			Content: content,
			Locator: store.Locator{
				Kind:   store.LocatorSymbol,
				Start:  tc.Locator.Start + startLineOffset - 1,
				End:    tc.Locator.End + startLineOffset - 1,
				Symbol: sym,
			},
		})
	}
	
	return chunks
}
