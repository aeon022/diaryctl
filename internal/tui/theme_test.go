package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/aeon022/missionctl-core/theme"
)

func TestThemeKeyOpensPickerWithLivePreview(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	orig := theme.Current()
	t.Cleanup(func() { _ = theme.Apply(orig) })

	m := &Model{width: 100, height: 30}
	before := titleStyle.GetForeground()
	m.handleList(tea.KeyPressMsg{Text: "T", Code: 'T'})
	if !m.themePicker.IsOpen() {
		t.Fatal("T opens the theme popup")
	}
	m.Update(tea.KeyPressMsg{Text: "j", Code: 'j'}) // previews the next theme live
	if theme.Current() == orig || titleStyle.GetForeground() == before {
		t.Error("moving in the popup previews the theme live and rebuilds the cached styles")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.themePicker.IsOpen() || m.message != "Theme: "+theme.Current() {
		t.Errorf("enter closes the popup and confirms the theme, got %q", m.message)
	}
}
