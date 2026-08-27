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
