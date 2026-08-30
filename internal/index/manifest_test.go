package index

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestManifestValidation(t *testing.T) {
	m := &Manifest{
		EmbeddingModel: "model-a",
		Dim:            768,
		PrefixScheme:   "v1",
		ChunkerVersion: 2,
	}

	err := m.CheckValidity("model-a", 768, "v1", 2)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	err = m.CheckValidity("model-b", 768, "v1", 2)
	if err != ErrManifestMismatch {
		t.Errorf("expected ErrManifestMismatch, got %v", err)
	}
}

func TestManifestChangeDetection(t *testing.T) {
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "test.txt")
	os.WriteFile(filePath, []byte("hello world"), 0644)
	
	info, _ := os.Stat(filePath)
	hash, _ := HashFile(filePath)

	m := &Manifest{
		Files: map[string]FileMeta{
			filePath: {
				Mtime:  info.ModTime().UnixNano(),
				Size:   info.Size(),
				Hash:   hash,
				Chunks: []string{"chunk1"},
			},
		},
	}

	// Unchanged
	changed, err := m.HasChanged(filePath, info)
	if err != nil || changed {
		t.Errorf("expected unchanged, got changed=%v err=%v", changed, err)
	}

	// Modify size
	os.WriteFile(filePath, []byte("hello world long"), 0644)
	info2, _ := os.Stat(filePath)
	changed, err = m.HasChanged(filePath, info2)
	if !changed {
		t.Errorf("expected changed due to size")
	}

	// Modify content but keep size and mtime different
	// Test hash comparison when size is unchanged but mtime differs
	os.WriteFile(filePath, []byte("hello there"), 0644) // same size 11
	// change mtime artificially
	os.Chtimes(filePath, time.Now(), time.Now().Add(1*time.Hour))
	
	info3, _ := os.Stat(filePath)
	changed, err = m.HasChanged(filePath, info3)
	if !changed {
		t.Errorf("expected changed due to hash mismatch")
	}

	// Restore original content, different mtime -> hash matches
	os.WriteFile(filePath, []byte("hello world"), 0644)
	os.Chtimes(filePath, time.Now(), time.Now().Add(2*time.Hour))
	
	info4, _ := os.Stat(filePath)
	changed, err = m.HasChanged(filePath, info4)
	if err != nil || changed {
		t.Errorf("expected unchanged because hash matches even with different mtime")
	}
}

func TestManifest_IsStale(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "project")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}

	fileA := filepath.Join(root, "a.txt")
	fileB := filepath.Join(root, "b.txt")
	os.WriteFile(fileA, []byte("content a"), 0644)
	os.WriteFile(fileB, []byte("content b"), 0644)

	infoA, _ := os.Stat(fileA)
	hashA, _ := HashFile(fileA)
	infoB, _ := os.Stat(fileB)
	hashB, _ := HashFile(fileB)

	cfg := testIndexConfig(".txt")

	m := &Manifest{
		Roots: []string{root},
		Files: map[string]FileMeta{
			fileA: {
				Mtime:  infoA.ModTime().UnixNano(),
				Size:   infoA.Size(),
				Hash:   hashA,
				Chunks: []string{"a-1"},
			},
			fileB: {
				Mtime:  infoB.ModTime().UnixNano(),
				Size:   infoB.Size(),
				Hash:   hashB,
				Chunks: []string{"b-1"},
			},
		},
	}

	// 1. Initially matching manifest: should not be stale
	stale, err := m.IsStale(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stale {
		t.Errorf("expected clean manifest to not be stale")
	}

	// 2. Added a new file: should be stale
	fileC := filepath.Join(root, "c.txt")
	os.WriteFile(fileC, []byte("content c"), 0644)
	stale, err = m.IsStale(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !stale {
		t.Errorf("expected manifest to be stale after adding file c.txt")
	}
	_ = os.Remove(fileC)

	// 3. Modified existing file size: should be stale
	os.WriteFile(fileA, []byte("content a modified longer"), 0644)
	stale, err = m.IsStale(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !stale {
		t.Errorf("expected manifest to be stale after modifying file a.txt size")
	}

	// Restore original fileA
	os.WriteFile(fileA, []byte("content a"), 0644)
	os.Chtimes(fileA, infoA.ModTime(), infoA.ModTime())

	// 4. Deleted file: should be stale
	os.Remove(fileB)
	stale, err = m.IsStale(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !stale {
		t.Errorf("expected manifest to be stale after deleting file b.txt")
	}
}

func TestManifest_TransientTouch(t *testing.T) {
	m := &Manifest{
		TransientRoots: map[string]int64{
			"/tmp/ephemeral": 1000,
		},
	}

	// Path inside ephemeral root
	touched := m.TouchPath("/tmp/ephemeral/sub/file.go")
	if !touched {
		t.Errorf("expected TouchPath to return true for path under transient root")
	}
	if m.TransientRoots["/tmp/ephemeral"] <= 1000 {
		t.Errorf("expected lastAccess to be updated, got %d", m.TransientRoots["/tmp/ephemeral"])
	}

	// Path outside ephemeral root
	touched = m.TouchPath("/other/path/file.go")
	if touched {
		t.Errorf("expected TouchPath to return false for path outside transient roots")
	}
}
