package clipboard

import (
	"os"
	"path/filepath"
	"testing"
)

func createFakeExe(t *testing.T, dir, name string) {
	path := filepath.Join(dir, name)
	// Create a simple bash script that does nothing and exits 0
	content := []byte("#!/bin/sh\nexit 0\n")
	err := os.WriteFile(path, content, 0755)
	if err != nil {
		t.Fatalf("failed to create fake exe %s: %v", name, err)
	}
}

func TestCopyBackendSelection(t *testing.T) {
	origPath := os.Getenv("PATH")
	defer os.Setenv("PATH", origPath)

	tmpDir := t.TempDir()

	// Test fallback chain
	
	// Case 1: wl-copy available
	dir1 := filepath.Join(tmpDir, "case1")
	os.MkdirAll(dir1, 0755)
	createFakeExe(t, dir1, "wl-copy")
	os.Setenv("PATH", dir1)
	
	mech, err := Copy("test")
	if err != nil {
		t.Fatalf("Copy failed: %v", err)
	}
	if mech != "wl-copy" {
		t.Errorf("Expected wl-copy, got %s", mech)
	}

	// Case 2: xclip available
	dir2 := filepath.Join(tmpDir, "case2")
	os.MkdirAll(dir2, 0755)
	createFakeExe(t, dir2, "xclip")
	os.Setenv("PATH", dir2)
	
	mech, err = Copy("test")
	if err != nil {
		t.Fatalf("Copy failed: %v", err)
	}
	if mech != "xclip" {
		t.Errorf("Expected xclip, got %s", mech)
	}

	// Case 3: xsel available
	dir3 := filepath.Join(tmpDir, "case3")
	os.MkdirAll(dir3, 0755)
	createFakeExe(t, dir3, "xsel")
	os.Setenv("PATH", dir3)
	
	mech, err = Copy("test")
	if err != nil {
		t.Fatalf("Copy failed: %v", err)
	}
	if mech != "xsel" {
		t.Errorf("Expected xsel, got %s", mech)
	}

	// Case 4: None available, expects osc52
	dir4 := filepath.Join(tmpDir, "case4")
	os.MkdirAll(dir4, 0755)
	os.Setenv("PATH", dir4)
	
	mech, err = Copy("test")
	if err != nil {
		t.Fatalf("Copy failed: %v", err)
	}
	if mech != "osc52" {
		t.Errorf("Expected osc52, got %s", mech)
	}
}
