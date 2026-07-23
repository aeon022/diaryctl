package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestHelpOverlay_OpenScrollClose(t *testing.T) {
	m := &Model{width: 100, height: 30}

	m.handleList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if m.view != helpView {
		t.Fatalf("expected helpView after '?', got %v", m.view)
	}
	if m.helpVP.TotalLineCount() == 0 {
		t.Fatal("expected help content to be populated")
	}

	before := m.helpVP.ScrollPercent()
	for i := 0; i < 5; i++ {
		m.handleHelp(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	}
	if m.helpVP.ScrollPercent() <= before {
		t.Errorf("expected scroll to advance after pressing j, stayed at %v", before)
	}

	m.handleHelp(tea.KeyMsg{Type: tea.KeyEsc})
	if m.view != listView {
		t.Errorf("expected esc to close help back to listView, got %v", m.view)
	}
}

func TestHelpOverlay_FitsWithinBackgroundHeight(t *testing.T) {
	m := &Model{width: 100, height: 30}
	m.openHelp()
	bgLines := len(strings.Split(m.viewList(), "\n"))
	if m.helpPopH > bgLines {
		t.Errorf("popup height %d exceeds background height %d", m.helpPopH, bgLines)
	}
}

func TestHelpOverlay_PopupContentSurvivesComposition(t *testing.T) {
	// diaryctl's list view has its own two side-by-side bordered panels
	// (heatmap + entry list). A naive "first border char per row" scan
	// mostly finds THOSE panels' own border (they start at column 0 on
	// almost every row) rather than the popup's — that check would pass
	// vacuously without verifying the popup's position at all, so this
	// checks something a corruption or mis-slice would actually break
	// instead: the popup's own unique footer text making it through
	// composition intact and undamaged.
	m := &Model{width: 100, height: 30}
	m.openHelp()

	out := m.View()
	if !strings.Contains(out, "close") {
		t.Errorf("expected the popup's footer text to survive compositing, got:\n%s", out)
	}
	if got, want := len(strings.Split(out, "\n")), m.height; got != want {
		t.Errorf("expected composited output to have exactly %d rows (padded to terminal height), got %d", want, got)
	}
}
