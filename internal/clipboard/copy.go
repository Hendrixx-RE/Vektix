package clipboard

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Copy copies text to the clipboard and returns the mechanism used.
// The OSC 52 fallback is written to stdout.
func Copy(text string) (string, error) {
	return CopyTo(os.Stdout, text)
}

// CopyTo is Copy with an explicit sink for the OSC 52 fallback sequence, so a
// caller emitting machine-readable output on stdout can divert the escape
// sequence to the terminal on stderr instead of corrupting its own payload.
func CopyTo(w io.Writer, text string) (string, error) {
	if _, err := exec.LookPath("wl-copy"); err == nil {
		cmd := exec.Command("wl-copy")
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return "wl-copy", nil
		}
	}

	if _, err := exec.LookPath("xclip"); err == nil {
		cmd := exec.Command("xclip", "-selection", "clipboard")
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return "xclip", nil
		}
	}

	if _, err := exec.LookPath("xsel"); err == nil {
		cmd := exec.Command("xsel", "--clipboard", "--input")
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return "xsel", nil
		}
	}

	// OSC 52
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	if _, err := fmt.Fprintf(w, "\x1b]52;c;%s\x07", encoded); err != nil {
		return "", err
	}
	return "osc52", nil
}
