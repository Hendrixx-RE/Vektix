package fileops

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Hendrixx-RE/Vektix/internal/config"
)

var secretPatterns = []string{
	".ssh",
	".gnupg",
	".aws/credentials",
	"*.pem",
	"*.key",
	".env*",
}

func matchesSecretPattern(path string) bool {
	normPath := filepath.ToSlash(path)
	parts := strings.Split(normPath, "/")

	for _, part := range parts {
		if part == ".ssh" || part == ".gnupg" {
			return true
		}
		if matched, _ := filepath.Match("*.pem", part); matched {
			return true
		}
		if matched, _ := filepath.Match("*.key", part); matched {
			return true
		}
		if matched, _ := filepath.Match(".env*", part); matched {
			return true
		}
	}

	if strings.Contains(normPath, "/.aws/credentials") || strings.HasPrefix(normPath, ".aws/credentials") {
		return true
	}

	return false
}

// ResolvePath resolves symlinks, makes it absolute, and enforces confinement and secrets.
func ResolvePath(targetPath string, explicitUnsafe bool, cfg *config.Config) (string, error) {
	absPath, err := filepath.Abs(targetPath)
	if err != nil {
		return "", fmt.Errorf("failed to make path absolute: %w", err)
	}

	evalPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			evalPath = absPath
		} else {
			return "", fmt.Errorf("failed to eval symlinks: %w", err)
		}
	}

	if !explicitUnsafe {
		// Access to secret files is blocked by default. This gate can be bypassed in two ways:
		// 1. Globally, by a human setting allow_secrets = true in their config.toml
		// 2. Per-invocation, by a human passing the --unsafe flag
		// Both are deliberate, human-controlled opt-ins. The unsafe flag is only ever supplied from CLI parsing, never from the model.
		if (cfg == nil || !cfg.Safety.AllowSecrets) && matchesSecretPattern(evalPath) {
			return "", fmt.Errorf("path matches secrets denylist, requires explicit unsafe flag: %s", targetPath)
		}

		if cfg != nil && cfg.Safety.ConfineToRoots {
			cwd, err := os.Getwd()
			if err != nil {
				return "", fmt.Errorf("failed to get cwd: %w", err)
			}

			cwdAbs, err := filepath.Abs(cwd)
			if err != nil {
				return "", fmt.Errorf("failed to abs cwd: %w", err)
			}
			cwdEval, err := filepath.EvalSymlinks(cwdAbs)
			if err != nil {
				if os.IsNotExist(err) {
					cwdEval = cwdAbs
				} else {
					return "", fmt.Errorf("failed to eval cwd symlinks: %w", err)
				}
			}

			roots := []string{cwdEval}
			for _, root := range cfg.Index.IndexDirs {
				exp, err := config.ExpandPath(root)
				if err != nil {
					continue
				}
				rootAbs, err := filepath.Abs(exp)
				if err != nil {
					continue
				}
				rootEval, err := filepath.EvalSymlinks(rootAbs)
				if err != nil {
					if os.IsNotExist(err) {
						rootEval = rootAbs
					} else {
						continue
					}
				}
				roots = append(roots, rootEval)
			}

			confined := false
			for _, root := range roots {
				if isSubpath(evalPath, root) {
					confined = true
					break
				}
			}

			if !confined {
				return "", fmt.Errorf("path is outside indexed roots and CWD, requires explicit unsafe flag: %s", targetPath)
			}
		}
	}

	return evalPath, nil
}

func isSubpath(target, base string) bool {
	if target == base {
		return true
	}
	baseWithSep := base
	if !strings.HasSuffix(baseWithSep, string(filepath.Separator)) {
		baseWithSep += string(filepath.Separator)
	}
	return strings.HasPrefix(target, baseWithSep)
}
