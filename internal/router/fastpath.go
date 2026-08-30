package router

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Hendrixx-RE/Vektix/internal/session"
)

type Intent struct {
	Action string `json:"action"`
	Query  string `json:"query,omitempty"`
	Path   string `json:"path,omitempty"`
	Lines  string `json:"lines,omitempty"`
}

type Pattern struct {
	Re     *regexp.Regexp
	Action string
	Guard  func(string) bool
}

var knownExtensions = map[string]bool{
	".txt": true, ".md": true, ".pdf": true,
	".go": true, ".py": true, ".js": true, ".ts": true, ".rs": true, ".sh": true, ".c": true, ".java": true,
	".json": true, ".yaml": true, ".yml": true, ".toml": true,
}

// pathShaped: contains '/', OR has a known extension, OR resolves to an existing file, OR is a single token with no spaces.
func pathShaped(s string) bool {
	if strings.Contains(s, "/") {
		return true
	}
	ext := filepath.Ext(s)
	if ext != "" && knownExtensions[strings.ToLower(ext)] {
		return true
	}
	if _, err := os.Stat(s); err == nil {
		return true
	}
	if !strings.Contains(s, " ") {
		return true
	}
	return false
}

// globShaped: pathShaped, or contains '*', '?', or '['.
func globShaped(s string) bool {
	if pathShaped(s) {
		return true
	}
	if strings.ContainsAny(s, "*?[") {
		return true
	}
	return false
}

// pathShapedOrRef: pathShaped, or an ordinal/session reference recognized by session.IsExplicitRef.
func pathShapedOrRef(s string) bool {
	if pathShaped(s) {
		return true
	}
	return session.IsExplicitRef(s)
}

var fastPatterns = []Pattern{
	{Re: regexp.MustCompile(`^open\s+(.+)$`), Action: "open", Guard: pathShaped},
	{Re: regexp.MustCompile(`^(?:read|show|cat)\s+(.+)$`), Action: "read", Guard: pathShaped},
	{Re: regexp.MustCompile(`^(?:ls|list)\s+(.+)$`), Action: "list", Guard: pathShaped},
	{Re: regexp.MustCompile(`^head\s+(-?\d+)\s+(.+)$`), Action: "read", Guard: pathShaped},
	{Re: regexp.MustCompile(`^tail\s+(-?\d+)\s+(.+)$`), Action: "read", Guard: pathShaped},
	{Re: regexp.MustCompile(`^find\s+(.+)$`), Action: "locate", Guard: globShaped},
	{Re: regexp.MustCompile(`^copy\s+(.+)$`), Action: "copy", Guard: pathShapedOrRef},
}

// ParseFastPath attempts to parse the input via guarded regex patterns.
// It returns a pointer to an Intent if successful, or nil if all guards fail (falling through to Tier 2).
func ParseFastPath(input string) *Intent {
	input = strings.TrimSpace(input)

	for _, p := range fastPatterns {
		matches := p.Re.FindStringSubmatch(input)
		if len(matches) == 0 {
			continue
		}

		// matches[1] might be lines or path, depending on the regex.
		// For head/tail, matches[1] is lines, matches[2] is path.
		// For others, matches[1] is path/query.
		var lines, pathArg string
		if len(matches) == 3 {
			lines = matches[1]
			pathArg = matches[2]
		} else {
			pathArg = matches[1]
		}

		if !p.Guard(pathArg) {
			continue
		}

		intent := &Intent{
			Action: p.Action,
		}

		if p.Action == "locate" {
			intent.Query = pathArg // for locate, 'find foo' sets query to foo
		} else {
			intent.Path = pathArg
		}

		if lines != "" {
			intent.Lines = lines
		}

		return intent
	}

	return nil
}
