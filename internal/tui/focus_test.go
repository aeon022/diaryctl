package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestFocusReloadsListWhenStale(t *testing.T) {
	m, _, _ := newState(t, "a", "b")
	m.lastLoad = time.Now().Add(-time.Minute)
	_, cmd := m.Update(tea.FocusMsg{})
	if cmd == nil {
		t.Fatal("focus on the list with stale data must reload")
	}
	if time.Since(m.lastLoad) > time.Second {
		t.Error("issuing a reload must restamp lastLoad")
	}
	if _, cmd = m.Update(tea.FocusMsg{}); cmd != nil {
		t.Error("a second focus right away must not reload again")
	}
}

func TestFocusNeverTouchesEditorOrInput(t *testing.T) {
	cases := map[string]func(*Model){
		"editor":         func(m *Model) { m.view = editorView; m.editorDirty = true },
		"detail":         func(m *Model) { m.view = detailView },
		"help":           func(m *Model) { m.view = helpView },
		"repos":          func(m *Model) { m.view = repoView },
		"search":         func(m *Model) { m.searching = true },
		"palette":        func(m *Model) { m.inPalette = true },
		"delete confirm": func(m *Model) { m.confirmDelete = true },
		"still loading":  func(m *Model) { m.loading = true },
	}
	for name, set := range cases {
		t.Run(name, func(t *testing.T) {
			m, _, _ := newState(t, "a")
			m.lastLoad = time.Now().Add(-time.Minute)
			set(m)
			if _, cmd := m.Update(tea.FocusMsg{}); cmd != nil {
				t.Errorf("%s: focus must not reload", name)
			}
		})
	}
}

func TestEditorTextSurvivesFocus(t *testing.T) {
	m, _, _ := newState(t, "a")
	keys(t, m, "e") // open the editor on the entry
	if m.view != editorView {
		t.Fatalf("e should open the editor, view=%v", m.view)
	}
	m.ta.SetValue("unsaved thoughts")
	m.lastLoad = time.Now().Add(-time.Minute)
	run(t, m, tea.FocusMsg{})
	if got := m.ta.Value(); got != "unsaved thoughts" {
		t.Errorf("focus clobbered editor text: %q", got)
	}
}

func TestReloadClampsCursorWhenEntriesShrink(t *testing.T) {
	m, s, days := newState(t, "a", "b", "c")
	m.cursor = 2
	if err := s.DeleteEntry(days[1]); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteEntry(days[2]); err != nil {
		t.Fatal(err)
	}
	run(t, m, cmdLoadEntries(s)()) // what a focus reload delivers
	if m.cursor != 0 || len(m.entries) != 1 {
		t.Errorf("cursor=%d entries=%d, want cursor clamped to 0 with 1 entry", m.cursor, len(m.entries))
	}
}

func TestCopyUsesOSC52AndPbcopy(t *testing.T) {
	cmd := copyToClipboardCmd("2026-10-05")
	if cmd == nil {
		t.Fatal("no cmd")
	}
	// tea.Batch's cmd only assembles the BatchMsg — it doesn't run pbcopy.
	b, ok := cmd().(tea.BatchMsg)
	if !ok || len(b) != 2 {
		t.Fatalf("want a BatchMsg of 2 (OSC 52 + pbcopy), got %T len=%d", cmd(), len(b))
	}
}

func TestViewReportsFocus(t *testing.T) {
	m, _, _ := newState(t, "a")
	if !m.View().ReportFocus {
		t.Error("View must set ReportFocus or FocusMsg never arrives")
	}
}
