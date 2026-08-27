package index

import (
	"fmt"
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/Hendrixx-RE/Vektix/internal/config"
)

type WalkFunc func(path string, info fs.FileInfo) error

type Walker struct {
	cfg          *config.IndexConfig
	rootIgnorer  *Ignorer
	visitedInodes map[string]bool
}

func NewWalker(cfg *config.IndexConfig) *Walker {
	return &Walker{
		cfg:           cfg,
		rootIgnorer:   NewRootIgnorer(&cfg.Exclude, ""),
		visitedInodes: make(map[string]bool),
	}
}

func (w *Walker) Walk(root string, fn WalkFunc) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	w.rootIgnorer.dir = absRoot
	return w.walk(absRoot, w.rootIgnorer, fn)
}

func (w *Walker) walk(dir string, ig *Ignorer, fn WalkFunc) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	// Push the new .vektixignore context for this directory
	currentIg := ig.Push(dir)

	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(dir, name)
		
		info, err := entry.Info()
		if err != nil {
			continue // Skip files we can't stat
		}

		isDir := entry.IsDir()
		isSymlink := info.Mode()&fs.ModeSymlink != 0

		if isSymlink {
			if !w.cfg.FollowSymlinks {
				continue
			}
			
			// Resolve symlink
			targetPath, err := filepath.EvalSymlinks(path)
			if err != nil {
				continue
			}
			
			targetInfo, err := os.Stat(targetPath)
			if err != nil {
				continue
			}
			
			info = targetInfo
			isDir = targetInfo.IsDir()
			path = targetPath
		}

		// Cycle detection
		stat, ok := info.Sys().(*syscall.Stat_t)
		if ok {
			// dev, inode pair as string
			inodeKey := fmt.Sprintf("%d:%d", stat.Dev, stat.Ino)
			if w.visitedInodes[inodeKey] {
				continue // already visited, skip
			}
			w.visitedInodes[inodeKey] = true
		}

		if currentIg.ShouldIgnore(path, isDir) {
			continue
		}

		if isDir {
			if err := w.walk(path, currentIg, fn); err != nil {
				return err
			}
		} else {
			if !w.isAllowedFile(path, info) {
				continue
			}
			
			if err := fn(path, info); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *Walker) isAllowedFile(path string, info fs.FileInfo) bool {
	// Size check
	if info.Size() > int64(w.cfg.MaxFileSizeMB)*1024*1024 {
		return false
	}

	// Extension check
	ext := strings.ToLower(filepath.Ext(path))
	allowed := false
	for _, allowedExt := range w.cfg.Extensions {
		if ext == allowedExt {
			allowed = true
			break
		}
	}
	if !allowed {
		return false
	}

	// Binary sniff (first 8KB)
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()

	buf := make([]byte, 8192)
	n, err := io.ReadFull(file, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return false
	}
	buf = buf[:n]

	if bytes.IndexByte(buf, 0) != -1 {
		return false // NUL byte found
	}

	if !utf8.Valid(buf) {
		return false // Invalid UTF-8
	}

	return true
}
