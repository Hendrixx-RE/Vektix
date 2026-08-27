package fileops

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSplitEditorCmd(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"code -w", []string{"code", "-w"}},
		{"nvim -p", []string{"nvim", "-p"}},
		{"'code' -w", []string{"code", "-w"}},
		{`"C:\Program Files\code.exe" -w`, []string{`C:\Program Files\code.exe`, "-w"}},
		{`my\ editor --arg`, []string{`my editor`, "--arg"}}, // escaped space
		{"vim", []string{"vim"}},
		{"", []string{}},
	}

	for _, tt := range tests {
		got := splitEditorCmd(tt.input)
		if len(got) == 0 && len(tt.expected) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, tt.expected) {
			t.Errorf("splitEditorCmd(%q) = %v, expected %v", tt.input, got, tt.expected)
		}
	}
}

func TestNoWriteSyscalls(t *testing.T) {
	packages := []string{
		".",
		"../clipboard",
	}

	disallowed := []string{
		"os.Create",
		"os.WriteFile",
		"os.Remove",
		"os.Rename",
	}

	for _, pkg := range packages {
		files, err := filepath.Glob(filepath.Join(pkg, "*.go"))
		if err != nil {
			t.Fatalf("failed to glob package %s: %v", pkg, err)
		}

		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue // Skip test files
			}

			content, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("failed to read file %s: %v", file, err)
			}
			contentStr := string(content)

			for _, bad := range disallowed {
				if strings.Contains(contentStr, bad) {
					t.Errorf("File %s contains forbidden write function: %s", file, bad)
				}
			}
		}
	}
}
