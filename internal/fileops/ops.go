package fileops

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/Hendrixx-RE/Vektix/internal/config"
)

// ReadFile safely reads a file.
func ReadFile(path string, explicitUnsafe bool, cfg *config.Config) ([]byte, error) {
	safePath, err := ResolvePath(path, explicitUnsafe, cfg)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(safePath)
}

func splitEditorCmd(editor string) []string {
	var args []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escape := false

	for _, r := range editor {
		if escape {
			// If we are in double quotes, backslash only escapes specific chars
			if inDouble && r != '"' && r != '\\' && r != '$' && r != '`' {
				current.WriteRune('\\')
			}
			current.WriteRune(r)
			escape = false
			continue
		}

		if r == '\\' {
			if inSingle {
				current.WriteRune(r)
			} else {
				escape = true
			}
			continue
		}

		if r == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}

		if r == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}

		if (r == ' ' || r == '\t') && !inSingle && !inDouble {
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
			continue
		}

		current.WriteRune(r)
	}

	if escape {
		current.WriteRune('\\')
	}

	if current.Len() > 0 {
		args = append(args, current.String())
	}

	return args
}

// Open launches the editor for the given path.
func Open(path string, explicitUnsafe bool, cfg *config.Config) error {
	safePath, err := ResolvePath(path, explicitUnsafe, cfg)
	if err != nil {
		return err
	}

	editor := ""
	if cfg != nil {
		editor = cfg.General.Editor
	}
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}

	if editor != "" {
		args := splitEditorCmd(editor)
		if len(args) > 0 {
			cmdName := args[0]
			cmdArgs := append(args[1:], "--", safePath)
			cmd := exec.Command(cmdName, cmdArgs...)
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run()
		}
	}

	// Fallback to xdg-open
	if _, err := exec.LookPath("xdg-open"); err == nil {
		cmd := exec.Command("xdg-open", "--", safePath)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	// Degrade to simply printing the path
	fmt.Println(safePath)
	return nil
}
