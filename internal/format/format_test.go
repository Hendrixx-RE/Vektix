package format

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDisplayPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("skipping home dir test when UserHomeDir is not available")
	}

	if p := DisplayPath(home); p != "~" {
		t.Errorf("expected '~', got %q", p)
	}

	sub := filepath.Join(home, "projects", "vektix")
	if p := DisplayPath(sub); p != "~/projects/vektix" {
		t.Errorf("expected '~/projects/vektix', got %q", p)
	}

	nonHome := "/var/log/syslog"
	if p := DisplayPath(nonHome); p != nonHome {
		t.Errorf("expected %q, got %q", nonHome, p)
	}
}

func TestHumanInt(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, "0"},
		{42, "42"},
		{999, "999"},
		{1000, "1,000"},
		{1234567, "1,234,567"},
		{-500, "-500"},
	}

	for _, tc := range tests {
		if got := HumanInt(tc.input); got != tc.want {
			t.Errorf("HumanInt(%d) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{500, "500B"},
		{1024, "1.0KB"},
		{1024 * 1024 * 5, "5.0MB"},
	}

	for _, tc := range tests {
		if got := HumanBytes(tc.input); got != tc.want {
			t.Errorf("HumanBytes(%d) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	if d := FormatDuration(50 * time.Millisecond); d != "50ms" {
		t.Errorf("expected '50ms', got %q", d)
	}
	if d := FormatDuration(1500 * time.Millisecond); d != "1.5s" {
		t.Errorf("expected '1.5s', got %q", d)
	}
}
