package tui

import (
	"strings"
	"testing"

	"github.com/Hendrixx-RE/Vektix/internal/config"
	"github.com/Hendrixx-RE/Vektix/internal/format"
)

func TestStatus_ResolveScopeState(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Index.IndexDirs = []string{"/tmp/test-project"}

	// 1. Scoped to CWD under root
	st := ResolveScopeState(&cfg, "/tmp/test-project/sub", "", false, 100, func(s string) int {
		if s == "/tmp/test-project/sub" {
			return 25
		}
		return 100
	})

	if st.Global {
		t.Errorf("expected scoped state, got global")
	}
	if st.Path != "/tmp/test-project/sub" {
		t.Errorf("expected path /tmp/test-project/sub, got %s", st.Path)
	}
	if st.Chunks != 25 {
		t.Errorf("expected 25 chunks in scope, got %d", st.Chunks)
	}
	if st.Total != 100 {
		t.Errorf("expected 100 total chunks, got %d", st.Total)
	}

	// 2. Global override
	stGlobal := ResolveScopeState(&cfg, "/tmp/test-project/sub", "", true, 100, func(s string) int { return 25 })
	if !stGlobal.Global || stGlobal.Path != "" {
		t.Errorf("expected global state, got %+v", stGlobal)
	}
	if stGlobal.Name() != "global" {
		t.Errorf("expected name 'global', got %s", stGlobal.Name())
	}
}

func TestStatus_FormattingAndRendering(t *testing.T) {
	st := ScopeState{
		Path:     "/home/user/docs",
		Global:   false,
		Chunks:   412,
		Total:    1000,
		HasIndex: true,
	}

	desc := st.Describe()
	if !strings.Contains(desc, "412 chunks") {
		t.Errorf("unexpected describe output: %s", desc)
	}

	banner := st.Banner()
	if !strings.Contains(banner, "412 of 1,000 chunks") {
		t.Errorf("unexpected banner output: %s", banner)
	}

	theme := DefaultTheme()
	rendered := RenderStatusBar(80, st, theme)
	if !strings.Contains(rendered, "VEKTIX") || !strings.Contains(rendered, "scope:") {
		t.Errorf("unexpected rendered status bar: %s", rendered)
	}
}

func TestStatus_HumanIntAndDisplayPath(t *testing.T) {
	if s := format.HumanInt(1234567); s != "1,234,567" {
		t.Errorf("expected '1,234,567', got %s", s)
	}
	if s := format.HumanInt(42); s != "42" {
		t.Errorf("expected '42', got %s", s)
	}
	if s := format.HumanInt(0); s != "0" {
		t.Errorf("expected '0', got %s", s)
	}
}
