package index

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/Hendrixx-RE/Vektix/internal/config"
)

// Ignorer handles the three-layer exclusion system.
type Ignorer struct {
	cfgExclude *config.ExcludeConfig
	rules      []ignoreRule
	parent     *Ignorer
	dir        string // The absolute path of the directory this Ignorer belongs to (for anchored rules)
}

type ignoreRule struct {
	pattern  string
	negate   bool
	dirOnly  bool
	anchored bool
}

// NewRootIgnorer creates the top-level ignorer with config rules.
func NewRootIgnorer(cfg *config.ExcludeConfig, rootDir string) *Ignorer {
	return &Ignorer{
		cfgExclude: cfg,
		dir:        rootDir,
	}
}

// Push loads a .vektixignore file from the given directory (if it exists)
// and returns a new Ignorer that includes its rules, linking back to the parent.
// If no file exists, it returns a new Ignorer node or just the parent?
// Returning a new node is safer.
func (ig *Ignorer) Push(dir string) *Ignorer {
	child := &Ignorer{
		cfgExclude: ig.cfgExclude,
		parent:     ig,
		dir:        dir,
	}
	
	ignorePath := filepath.Join(dir, ".vektixignore")
	file, err := os.Open(ignorePath)
	if err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			child.rules = append(child.rules, parseIgnoreRule(line))
		}
	}
	return child
}

func parseIgnoreRule(line string) ignoreRule {
	rule := ignoreRule{}
	if strings.HasPrefix(line, "!") {
		rule.negate = true
		line = line[1:]
	}
	if strings.HasSuffix(line, "/") {
		rule.dirOnly = true
		line = line[:len(line)-1]
	}
	
	if strings.Contains(line, "/") && !strings.HasPrefix(line, "/") {
		rule.anchored = true
	} else if strings.HasPrefix(line, "/") {
		rule.anchored = true
		line = line[1:]
	}
	
	rule.pattern = line
	return rule
}

// ShouldIgnore returns true if the file or directory should be ignored.
func (ig *Ignorer) ShouldIgnore(absPath string, isDir bool) bool {
	baseName := filepath.Base(absPath)
	
	// 1. Hardcoded defaults (Layer 1)
	if isHardcodedIgnored(absPath, isDir, baseName) {
		return true
	}

	// 2. Config-level excludes (Layer 2)
	if ig.isConfigIgnored(absPath, isDir, baseName) {
		return true
	}

	// 3. .vektixignore rules (Layer 3)
	// We evaluate rules from root to leaf, because a negate rule in a subdirectory
	// overrides an exclusion rule in a parent directory, meaning the last matching rule wins.
	
	ignored := false
	
	var nodes []*Ignorer
	for curr := ig; curr != nil; curr = curr.parent {
		nodes = append([]*Ignorer{curr}, nodes...) // prepend
	}
	
	for _, node := range nodes {
		for _, rule := range node.rules {
			if rule.dirOnly && !isDir {
				continue
			}
			
			match := false
			if rule.anchored {
				// Anchored: pattern matches relative to node.dir
				rel, err := filepath.Rel(node.dir, absPath)
				if err == nil && !strings.HasPrefix(rel, "..") {
					// Compare using filepath.Match
					// We might need to match path prefixes. If rule is "archive/*", it matches "archive/file.txt".
					// filepath.Match doesn't do subdirectories by default unless we match precisely.
					// Let's implement a simpler glob match or just use filepath.Match on the exact rel,
					// or strings.HasPrefix for directories.
					match = matchPattern(rule.pattern, rel)
				}
			} else {
				// Unanchored: matches baseName, or any parent directory name?
				// Typically unanchored matches the base name.
				match = matchPattern(rule.pattern, baseName)
			}
			
			if match {
				ignored = !rule.negate
			}
		}
	}

	return ignored
}

func matchPattern(pattern, name string) bool {
	matched, err := filepath.Match(pattern, name)
	if err == nil && matched {
		return true
	}
	// Also check if name is under a directory that matches the pattern (for anchored paths)
	// If pattern is "drafts" and name is "drafts/file.txt"
	if strings.HasPrefix(name, pattern+"/") {
		return true
	}
	return false
}

func (ig *Ignorer) isConfigIgnored(absPath string, isDir bool, baseName string) bool {
	if ig.cfgExclude == nil {
		return false
	}
	
	if isDir {
		for _, d := range ig.cfgExclude.Dirs {
			if matchPattern(d, baseName) {
				return true
			}
		}
	} else {
		for _, f := range ig.cfgExclude.Files {
			if matchPattern(f, baseName) {
				return true
			}
		}
	}
	
	for _, p := range ig.cfgExclude.Paths {
		// Replace ~ with home dir if needed, but config loading should have expanded it?
		// We'll just do a simple Contains or prefix match for now, or match on absolute path
		pExpanded, _ := config.ExpandPath(p)
		if strings.HasPrefix(absPath, pExpanded) {
			return true
		}
	}
	
	return false
}

func isHardcodedIgnored(absPath string, isDir bool, baseName string) bool {
	if !isDir {
		ext := strings.ToLower(filepath.Ext(baseName))
		switch ext {
		case ".jpg", ".png", ".gif", ".mp4", ".mp3", ".zip", ".tar", ".gz", ".7z":
			return true
		case ".o", ".so", ".exe", ".bin", ".dylib", ".class", ".pyc", ".wasm":
			return true
		case ".pem", ".key":
			return true
		}
		
		if baseName == ".DS_Store" || baseName == "Thumbs.db" || baseName == "desktop.ini" {
			return true
		}
		
		if strings.HasPrefix(baseName, ".env") {
			return true
		}
		
		// .aws/credentials
		if baseName == "credentials" {
			parent := filepath.Base(filepath.Dir(absPath))
			if parent == ".aws" {
				return true
			}
		}
	} else {
		switch baseName {
		case ".git", ".hg", ".svn", ".ssh", ".gnupg":
			return true
		}
	}
	return false
}
