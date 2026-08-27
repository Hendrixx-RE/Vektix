package index

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
)

type FileMeta struct {
	Mtime  int64    `json:"mtime"`
	Size   int64    `json:"size"`
	Hash   string   `json:"hash"`
	Chunks []string `json:"chunks"`
}

type Manifest struct {
	EmbeddingModel string              `json:"embedding_model"`
	Dim            int                 `json:"dim"`
	PrefixScheme   string              `json:"prefix_scheme"`
	ChunkerVersion int                 `json:"chunker_version"`
	Roots          []string            `json:"roots"`
	Files          map[string]FileMeta `json:"files"`
	DirCounts      map[string]int      `json:"dir_counts"`
}

var ErrManifestMismatch = errors.New("index manifest mismatch: please run 'vektix reindex'")

// CheckValidity compares critical fields. If they mismatch, it returns an error pointing to 'vektix reindex'.
func (m *Manifest) CheckValidity(model string, dim int, prefix string, chunkerVer int) error {
	if m.EmbeddingModel != model || m.Dim != dim || m.PrefixScheme != prefix || m.ChunkerVersion != chunkerVer {
		return ErrManifestMismatch
	}
	return nil
}

// HasChanged returns true if the file on disk is different from the stored metadata.
func (m *Manifest) HasChanged(path string, info os.FileInfo) (bool, error) {
	meta, ok := m.Files[path]
	if !ok {
		return true, nil // newly added
	}

	if info.Size() != meta.Size {
		return true, nil
	}

	if info.ModTime().UnixNano() != meta.Mtime {
		// Tiebreaker: content hash
		hash, err := HashFile(path)
		if err != nil {
			return true, err // Error reading file, treat as changed
		}
		if hash != meta.Hash {
			return true, nil
		}
	}

	return false, nil
}

// HashFile computes the SHA256 hash of a file for tiebreaker change detection.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ScopeFraction returns the proportion of total chunks that fall under the given scope directory.
func (m *Manifest) ScopeFraction(dirPath string) float64 {
	totalChunks := m.DirCounts[""]
	if totalChunks == 0 {
		// Compute total chunks if empty key doesn't exist
		for _, meta := range m.Files {
			totalChunks += len(meta.Chunks)
		}
		if totalChunks == 0 {
			return 0
		}
	}

	scopeCount := m.DirCounts[dirPath]
	return float64(scopeCount) / float64(totalChunks)
}

// LoadManifest reads and parses a manifest file.
func LoadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m.Files == nil {
		m.Files = make(map[string]FileMeta)
	}
	if m.DirCounts == nil {
		m.DirCounts = make(map[string]int)
	}
	return &m, nil
}

// SaveManifest writes the manifest to a file.
func (m *Manifest) SaveManifest(path string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
