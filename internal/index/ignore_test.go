package index

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Hendrixx-RE/Vektix/internal/config"
)

func TestIgnorePrecedence(t *testing.T) {
	tmp := t.TempDir()

	// Create directories
	dirs := []string{
		"node_modules",
		".git",
		"src",
		"src/drafts",
		"src/drafts/archive",
	}
	for _, d := range dirs {
		os.MkdirAll(filepath.Join(tmp, d), 0755)
	}

	// Create a .vektixignore in src
	ignoreContent := "drafts/\n!drafts/archive/\n*.bak\n"
	os.WriteFile(filepath.Join(tmp, "src", ".vektixignore"), []byte(ignoreContent), 0644)

	cfg := &config.ExcludeConfig{
		Dirs: []string{"node_modules"},
	}

	ignorer := NewRootIgnorer(cfg, tmp)

	tests := []struct {
		path  string
		isDir bool
		want  bool
	}{
		{"node_modules", true, true}, // Config ignore
		{".git", true, true},         // Hardcoded
		{"src", true, false},         // Allowed
	}

	for _, tt := range tests {
		absPath := filepath.Join(tmp, tt.path)
		if got := ignorer.ShouldIgnore(absPath, tt.isDir); got != tt.want {
			t.Errorf("ShouldIgnore(%q, %v) = %v, want %v", tt.path, tt.isDir, got, tt.want)
		}
	}

	srcIgnorer := ignorer.Push(filepath.Join(tmp, "src"))

	tests2 := []struct {
		path  string
		isDir bool
		want  bool
	}{
		{"src/main.go", false, false},
		{"src/file.bak", false, true},
		{"src/drafts", true, true},
		{"src/drafts/file.txt", false, false}, // inherited?
		{"src/drafts/archive", true, false},  // Negated
	}

	for _, tt := range tests2 {
		absPath := filepath.Join(tmp, tt.path)
		if got := srcIgnorer.ShouldIgnore(absPath, tt.isDir); got != tt.want {
			t.Errorf("ShouldIgnore(%q, %v) = %v, want %v", tt.path, tt.isDir, got, tt.want)
		}
	}
}
