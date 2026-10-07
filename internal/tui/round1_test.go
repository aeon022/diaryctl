package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/aeon022/diaryctl/internal/store"
	"github.com/aeon022/missionctl-core/tuitest"
	"github.com/charmbracelet/x/ansi"
)

// longTitleModel has AI-generated entries whose titles are far too long for
// any panel and contain multi-byte characters (em dash, umlauts) — the exact
// case where the "[AI]" tag used to wrap onto its own line.
func longTitleModel(t *testing.T, w, h int) *Model {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DIARYCTL_DATA_DIR", "")
	s, err := store.Open(filepath.Join(t.TempDir(), "diary.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	long := "Entwickler- & Studien-Tagebuch — Meilenstein-Freigabe, Vorweihnachts-Sprint & Forschungstagebuch-Architektur über Größen"
	for i := 0; i < 4; i++ {
		d := time.Date(2026, 9, 21-i, 0, 0, 0, 0, time.UTC)
		body := long + "\n\nText"
		if i == 2 {
			body = "# " + long + "\n\n#dev #study #architecture-and-planning\nText"
		}
		if err := s.SaveEntry(d, body, i%2 == 0); err != nil { // alternate AI / hand-written
			t.Fatal(err)
		}
	}
	m := New(s)
	m.width, m.height = w, h
	run(t, m, cmdLoadEntries(s)())
	return m
}

func TestEntryRowsStayOnOneLineWithAITagAtAnyWidth(t *testing.T) {
	// The regression, end to end: render the WHOLE list view (so the panel's
	// real inner width applies) and require every entry — selected or not — to
	// be a single line carrying its date and its AI tag.
	for _, w := range []int{60, 80, 110, 140} {
		for _, sel := range []int{0, 1} {
			m := longTitleModel(t, w, 30)
			m.cursor = sel
			lines := strings.Split(tuitest.Text(m), "\n")
			ai, dates := 0, 0
			for i, l := range lines {
				if lipgloss.Width(l) > w {
					t.Errorf("w=%d line %d is %d wide: %q", w, i, lipgloss.Width(l), l)
				}
				if strings.Contains(l, "2026-09-") {
					dates++
				}
				if strings.Contains(l, " AI ") { // the AI pill is " AI " (pill padding)
					ai++
					if !strings.Contains(l, "2026-09-") {
						t.Errorf("w=%d cursor=%d: the AI tag wrapped onto its own line: %q", w, sel, l)
					}
				}
			}
			// on a very narrow panel the tag is dropped rather than wrapped (0 is fine, a lone 1 is not)
			if dates != 4 || (ai != 2 && !(w < 80 && ai == 0)) {
				t.Errorf("w=%d cursor=%d: want 4 single-line entries / 2 AI tags, got %d / %d\n%s", w, sel, dates, ai, strings.Join(lines, "\n"))
			}
		}
	}
}

func TestEntryListRowsFitTheirPanelAtNarrowWidths(t *testing.T) {
	for _, w := range []int{40, 50} {
		m := longTitleModel(t, w, 30)
		inner := max(w-heatmapPanelW-6, 20) - 4
		for _, sel := range []int{0, 1} {
			m.cursor = sel
			for i, r := range strings.Split(m.renderEntryList(inner, 12), "\n") {
				if lipgloss.Width(r) > inner {
					t.Errorf("w=%d row %d is %d cells, inner width %d: %q", w, i, lipgloss.Width(r), inner, ansi.Strip(r))
				}
			}
		}
	}
}

func TestListHeaderCarriesCountsStreakAndDateAndNoRecentBlock(t *testing.T) {
	m := longTitleModel(t, 110, 30)
	m.todayLoaded, m.todayEvents = true, 2
	out := tuitest.Text(m)
	lines := strings.Split(out, "\n")
	head := lines[0]
	for _, want := range []string{"diaryctl", "Journal", "4 entries", "streak", "today 2 events", time.Now().Format("Mon 02 Jan")} {
		if !strings.Contains(head, want) {
			t.Errorf("header missing %q: %q", want, head)
		}
	}
	if strings.Contains(out, "Recent Entries") {
		t.Error("the duplicate 'Recent Entries' block must be gone")
	}
	if lipgloss.Width(head) != 110 {
		t.Errorf("header is %d wide, want 110", lipgloss.Width(head))
	}
}

func TestListFooterIsOneStatusbarLine(t *testing.T) {
	for _, w := range []int{60, 80, 110, 140} {
		m := longTitleModel(t, w, 30)
		lines := strings.Split(tuitest.Text(m), "\n")
		foot := lines[len(lines)-1]
		if lipgloss.Width(foot) > w {
			t.Errorf("w=%d footer is %d wide: %q", w, lipgloss.Width(foot), foot)
		}
		if !strings.Contains(foot, "enter open") || !strings.Contains(foot, "q quit") || !strings.Contains(foot, "? help") {
			t.Errorf("w=%d the key hints must stay: %q", w, foot)
		}
		if strings.Contains(foot, "j/k:navigate") {
			t.Errorf("old footer style: %q", foot)
		}
		if got := len(lines); got != 30-1+1 && got > 30 {
			t.Errorf("w=%d view is %d lines, terminal 30", w, got)
		}
	}
}

func TestDeleteConfirmationReplacesHintsInTheFooter(t *testing.T) {
	m := longTitleModel(t, 110, 30)
	mm, _ := tuitest.Keys(m, "d")
	lines := strings.Split(tuitest.Text(mm), "\n")
	if foot := lines[len(lines)-1]; !strings.Contains(foot, "Delete 2026-09-21? y = confirm") {
		t.Errorf("confirm prompt missing: %q", foot)
	}
}

func TestMouseClickStillSelectsRowAfterLayoutChange(t *testing.T) {
	m := longTitleModel(t, 110, 30)
	// the entries panel starts under the header (row 0), a blank row and the panel border
	x := heatmapPanelW + 6
	hit := m.rowHitTest(x, 4)
	if hit < 0 {
		t.Fatalf("a click on the first entry row must hit an entry, got %d", hit)
	}
}
