package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/aeon022/missionctl-core/theme"
)

func TestThemeKeySwitchesLiveAndSaves(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	orig := theme.Current()
	t.Cleanup(func() { _ = theme.Apply(orig) })

	m := &Model{width: 100, height: 30}
	before := titleStyle.GetForeground()
	m.handleList(tea.KeyPressMsg{Text: "T", Code: 'T'})

	if theme.Current() == orig {
		t.Fatalf("T must switch to the next theme, still %q", orig)
	}
	if titleStyle.GetForeground() == before {
		t.Error("cached styles must be rebuilt when the theme changes")
	}
	if m.message != "Theme: "+theme.Current() {
		t.Errorf("a short confirmation should name the theme, got %q", m.message)
	}
}
