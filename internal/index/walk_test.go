package index

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/Hendrixx-RE/Vektix/internal/config"
)

func TestWalker(t *testing.T) {
	tmp := t.TempDir()

	// Create directories
	dirs := []string{
		"node_modules",
		"src",
		"src/drafts",
		"src/drafts/archive",
		"src/.git",
	}
	for _, d := range dirs {
		os.MkdirAll(filepath.Join(tmp, d), 0755)
	}

	// Create files
	files := map[string][]byte{
		"src/main.go":           []byte("package main\n"),
		"src/file.bak":          []byte("backup\n"),
		"src/drafts/file.md":    []byte("draft\n"),
		"src/drafts/archive/a.md": []byte("archive\n"),
		"src/.git/config":       []byte("git config\n"),
		"src/image.png":         []byte("binarydata\x00\x00"),
		"src/valid.md":          []byte("valid utf8\n"),
		"src/invalid.md":        []byte("invalid \xff utf8\n"),
	}

	for p, content := range files {
		os.WriteFile(filepath.Join(tmp, p), content, 0644)
	}

	// .vektixignore
	ignoreContent := "drafts/\n*.bak\n"
	os.WriteFile(filepath.Join(tmp, "src", ".vektixignore"), []byte(ignoreContent), 0644)

	cfg := &config.IndexConfig{
		MaxFileSizeMB: 50,
		Extensions:    []string{".go", ".md"},
		Exclude: config.ExcludeConfig{
			Dirs: []string{"node_modules"},
		},
	}

	walker := NewWalker(cfg)

	visited := make(map[string]bool)
	err := walker.Walk(tmp, func(path string, info fs.FileInfo) error {
		rel, _ := filepath.Rel(tmp, path)
		visited[rel] = true
		return nil
	})

	if err != nil {
		t.Fatalf("Walk error: %v", err)
	}

	expected := []string{
		"src/main.go",
		"src/valid.md",
	}

	for _, e := range expected {
		if !visited[e] {
			t.Errorf("Expected to visit %q, but didn't", e)
		}
	}

	notExpected := []string{
		"node_modules",
		"src/file.bak",
		"src/drafts/file.md",
		"src/drafts/archive/a.md",
		"src/.git/config",
		"src/image.png",
		"src/invalid.md",
	}

	for _, n := range notExpected {
		if visited[n] {
			t.Errorf("Expected NOT to visit %q, but did", n)
		}
	}
}

func TestWalkerSymlinkLoop(t *testing.T) {
	tmp := t.TempDir()

	os.MkdirAll(filepath.Join(tmp, "a", "b"), 0755)
	os.WriteFile(filepath.Join(tmp, "a", "b", "file.md"), []byte("hello"), 0644)

	// Create a symlink loop
	os.Symlink(filepath.Join(tmp, "a"), filepath.Join(tmp, "a", "b", "loop"))

	cfg := &config.IndexConfig{
		MaxFileSizeMB:  50,
		Extensions:     []string{".md"},
		FollowSymlinks: true, // test following symlinks
	}

	walker := NewWalker(cfg)

	visitedCount := 0
	err := walker.Walk(tmp, func(path string, info fs.FileInfo) error {
		visitedCount++
		return nil
	})

	if err != nil {
		t.Fatalf("Walk error: %v", err)
	}

	if visitedCount != 1 { // Should only visit file.md once
		t.Errorf("Expected 1 visit, got %d", visitedCount)
	}
}
