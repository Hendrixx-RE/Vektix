package resolve

import (
	"path/filepath"
	"strings"
)

// ScopeResolution represents the result of determining the active scope.
type ScopeResolution struct {
	Path           string // The path to filter by (empty means global)
	RequiresPrompt bool   // True if CWD is unindexed and needs user prompt
}

// ResolveScope determines the search scope according to the resolution ladder.
func ResolveScope(cwd string, overrideScope string, isGlobal bool, configMode string, indexedRoots []string) (ScopeResolution, error) {
	if isGlobal || configMode == "global" {
		return ScopeResolution{Path: ""}, nil
	}

	if overrideScope != "" {
		absOverride, err := filepath.Abs(overrideScope)
		if err != nil {
			return ScopeResolution{}, err
		}
		return ScopeResolution{Path: absOverride}, nil
	}

	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		return ScopeResolution{}, err
	}

	// CWD mode or Auto mode
	// Check if absCwd is under any indexed root
	var bestRoot string
	for _, root := range indexedRoots {
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		if absCwd == absRoot || strings.HasPrefix(absCwd, absRoot+string(filepath.Separator)) {
			// Find the longest matching root in case of nested roots (though rare)
			if len(absRoot) > len(bestRoot) {
				bestRoot = absRoot
			}
		}
	}

	if bestRoot != "" {
		// CWD is under an indexed root
		return ScopeResolution{Path: absCwd}, nil
	}

	// CWD is outside every root
	if configMode == "cwd" {
		// If strict cwd mode, maybe we still prompt or just fail? The plan says:
		// "CWD outside all roots -> signal caller to prompt"
		return ScopeResolution{Path: absCwd, RequiresPrompt: true}, nil
	}

	// Auto mode default when outside: signal prompt
	return ScopeResolution{Path: absCwd, RequiresPrompt: true}, nil
}
