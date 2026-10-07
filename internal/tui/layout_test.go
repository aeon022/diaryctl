package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/aeon022/missionctl-core/tuitest"
	"github.com/charmbracelet/x/ansi"
)

// layoutModel has three distinct entries (newest first) at the given size.
func layoutModel(t *testing.T, w, h int) *Model {
	t.Helper()
	m, _, _ := newState(t,
		"# Alpha day\n\nalpha body line\n#dev",
		"# Bravo day\n\nbravo body line",
		"# Charlie day\n\ncharlie body line")
	m.width, m.height = w, h
	return m
}

// screenPos finds where `needle` is drawn: the screen row and the cell column.
func screenPos(t *testing.T, m *Model, needle string) (x, y int) {
	t.Helper()
	g := m.geom()
	for yy, l := range strings.Split(tuitest.Text(m), "\n") {
		// only the entries panel's columns: the Preview panel repeats titles
		seg := ansi.Cut(l, g.listX, g.listX+g.listW)
		if i := strings.Index(seg, needle); i >= 0 {
			return g.listX + lipgloss.Width(seg[:i]), yy
		}
	}
	t.Fatalf("%q is not on screen:\n%s", needle, tuitest.Text(m))
	return
}

func TestListViewFitsTheTerminalInEveryState(t *testing.T) {
	for _, w := range []int{40, 60, 80, 100, 140, 170} {
		for _, h := range []int{24, 30, 40} {
			for name, setup := range map[string]func(*Model){
				"entries": func(m *Model) {},
				"search":  func(m *Model) { m.searching, m.searchQuery = true, "alp" },
				"palette": func(m *Model) { m.inPalette, m.paletteQuery = true, "e" },
				"empty":   func(m *Model) { m.entries, m.loading = nil, false },
				"loading": func(m *Model) { m.entries, m.loading = nil, true },
			} {
				m := layoutModel(t, w, h)
				setup(m)
				lines := strings.Split(tuitest.Text(m), "\n")
				if len(lines) != h {
					t.Errorf("%s %dx%d: %d lines, want exactly the terminal height", name, w, h, len(lines))
				}
				for i, l := range lines {
					if lipgloss.Width(l) > w {
						t.Errorf("%s %dx%d: line %d is %d cells wide: %q", name, w, h, i, lipgloss.Width(l), l)
					}
				}
			}
		}
	}
}

func TestPreviewColumnOnlyFrom120Columns(t *testing.T) {
	for w, want := range map[int]bool{80: false, 110: false, 119: false, 120: true, 150: true, 170: true} {
		got := strings.Contains(tuitest.Text(layoutModel(t, w, 30)), "Preview")
		if got != want {
			t.Errorf("width %d: Preview panel shown = %v, want %v", w, got, want)
		}
	}
	// below 80 columns there is no heatmap column and no frame at all
	narrow := tuitest.Text(layoutModel(t, 60, 30))
	if strings.ContainsAny(narrow, "╭╮╰╯│") || strings.Contains(narrow, "Last 13 weeks") {
		t.Errorf("60 columns: list only, unframed:\n%s", narrow)
	}
	if !strings.Contains(narrow, "Entries") {
		t.Errorf("the divider still names the list:\n%s", narrow)
	}
}

func TestPreviewShowsTheSelectedEntryAndFollowsTheCursor(t *testing.T) {
	m := layoutModel(t, 150, 36)
	prev := func() string { // the right-hand panel only
		var b strings.Builder
		for _, l := range strings.Split(tuitest.Text(m), "\n") {
			if i := strings.Index(l, "Preview"); i >= 0 {
				b.WriteString(l[i:])
			} else if r := []rune(l); len(r) > m.geom().prevX {
				b.WriteString(string(r[m.geom().prevX:]))
			}
			b.WriteString("\n")
		}
		return b.String()
	}
	if p := prev(); !strings.Contains(p, "alpha body line") || strings.Contains(p, "bravo body line") {
		t.Errorf("cursor on entry 0 must preview entry 0:\n%s", p)
	}
	tuitest.Keys(m, "j")
	m.cursor = 1
	p := prev()
	if !strings.Contains(p, "bravo body line") || strings.Contains(p, "alpha body line") {
		t.Errorf("preview must follow the cursor:\n%s", p)
	}
	if !strings.Contains(p, "words") || !strings.Contains(p, time.Now().AddDate(0, 0, -1).Format("Mon 02 Jan 2006")) {
		t.Errorf("preview shows date and word count:\n%s", p)
	}
	if strings.Count(p, "Bravo day") != 1 {
		t.Errorf("the title appears once (not repeated as the first body line):\n%s", p)
	}
}

func TestClickSelectsWhereTheRowIsDrawn(t *testing.T) {
	for _, h := range []int{24, 30, 40} { // compact and spacious header tiers
		for _, w := range []int{80, 110, 150} {
			for _, mode := range []string{"plain", "search", "palette"} {
				m := layoutModel(t, w, h)
				switch mode {
				case "search":
					m.searching, m.searchQuery = true, ""
				case "palette":
					m.inPalette, m.paletteQuery = true, ""
				}
				for want, title := range []string{"Alpha day", "Bravo day", "Charlie day"} {
					x, y := screenPos(t, m, title)
					if got := m.rowHitTest(x, y); got != want {
						t.Errorf("%dx%d %s: a click on %q (x=%d y=%d) hits entry %d, want %d", w, h, mode, title, x, y, got, want)
					}
				}
				// outside the entries panel: heatmap column, header, below the rows
				if m.rowHitTest(2, 6) != -1 || m.rowHitTest(60, 0) != -1 || m.rowHitTest(60, h-1) != -1 {
					t.Errorf("%dx%d %s: clicks outside the entry rows must miss", w, h, mode)
				}
				if w >= 120 && m.rowHitTest(m.geom().prevX+4, 6) != -1 {
					t.Errorf("%dx%d: a click in the Preview panel must miss", w, h)
				}
			}
		}
	}
}

func TestClickGeometryHoldsWithoutBordersAndWhenScrolled(t *testing.T) {
	t.Setenv("MISSIONCTL_BORDERS", "none")
	m := layoutModel(t, 110, 30)
	x, y := screenPos(t, m, "Charlie day")
	if got := m.rowHitTest(x, y); got != 2 {
		t.Errorf("borders: none — click on the last entry hits %d, want 2", got)
	}
	// scrolled: many entries, cursor far down, the visible window starts later
	var bodies []string
	for i := 0; i < 60; i++ {
		bodies = append(bodies, fmt.Sprintf("# Entry number %02d\n\nbody", i))
	}
	m, _, _ = newState(t, bodies...)
	m.width, m.height = 110, 30
	m.cursor = 40
	x, y = screenPos(t, m, "Entry number 40")
	if got := m.rowHitTest(x, y); got != 40 {
		t.Errorf("scrolled window: click on the cursor row hits %d, want 40", got)
	}
}

func TestHeaderBlockTiersAndSelectionBar(t *testing.T) {
	spacious := strings.Split(tuitest.Text(layoutModel(t, 110, 30)), "\n")
	compact := strings.Split(tuitest.Text(layoutModel(t, 110, 24)), "\n")
	if strings.TrimSpace(spacious[2]) != "" || !strings.HasPrefix(spacious[3], "╭") {
		t.Errorf("height >= 30: header, rule, one blank line, then the panels:\n%s", strings.Join(spacious[:5], "\n"))
	}
	if !strings.HasPrefix(compact[2], "╭") {
		t.Errorf("height < 30: no blank line:\n%s", strings.Join(compact[:4], "\n"))
	}
	if !strings.Contains(spacious[0], time.Now().Format("Mon 02 Jan")) {
		t.Errorf("header date format: %q", spacious[0])
	}
	// the selected row carries the accent bar and the whole row is one selection
	m := layoutModel(t, 110, 30)
	_, y := screenPos(t, m, "Alpha day")
	row := strings.Split(m.viewList(), "\n")[y]
	if !strings.Contains(ansi.Strip(row), "▌ "+m.entries[0].Date.Format("2006-01-02")) {
		t.Errorf("selected row has the accent bar: %q", ansi.Strip(row))
	}
}

func TestSelectedRowKeepsDateReadable(t *testing.T) {
	// the dim date shares a color with the selection background in some themes;
	// ui.Row lifts dimmed text on selected rows, so the date is never invisible
	m := layoutModel(t, 110, 30)
	var sel string
	for _, l := range strings.Split(m.viewList(), "\n") {
		if strings.Contains(ansi.Strip(l), "▌ ") && strings.Contains(ansi.Strip(l), "Alpha day") {
			sel = l
		}
	}
	if sel == "" {
		t.Fatal("selected row not found")
	}
	if !strings.Contains(ansi.Strip(sel), m.entries[0].Date.Format("2006-01-02")) {
		t.Errorf("date missing in the selected row: %q", ansi.Strip(sel))
	}
}

// Detail, editor and repos share the list's chrome: header, one titled panel,
// a one-line footer, exactly the terminal height.
func TestSecondaryViewsShareTheSameChrome(t *testing.T) {
	for name, keys := range map[string][]string{
		"detail": {"enter"},
		"editor": {"e"},
		"repos":  {"r"},
	} {
		for _, w := range []int{50, 60, 80, 100, 140, 170} {
			for _, h := range []int{24, 30, 40} {
				m := layoutModel(t, w, h)
				mm, _ := tuitest.Keys(m, keys...)
				lines := strings.Split(tuitest.Text(mm), "\n")
				if len(lines) != h {
					t.Errorf("%s %dx%d: %d lines, want %d", name, w, h, len(lines), h)
				}
				for i, l := range lines {
					if lipgloss.Width(l) > w {
						t.Errorf("%s %dx%d: line %d is %d wide", name, w, h, i, lipgloss.Width(l))
					}
				}
				if !strings.Contains(lines[0], "diaryctl") {
					t.Errorf("%s %dx%d: header missing: %q", name, w, h, lines[0])
				}
				if name != "editor" && !strings.Contains(lines[len(lines)-1], "esc") {
					t.Errorf("%s %dx%d: footer lacks esc hint: %q", name, w, h, lines[len(lines)-1])
				}
			}
		}
	}
}

func TestEditorTextSurvivesResize(t *testing.T) {
	m := layoutModel(t, 100, 30)
	mm, _ := tuitest.Keys(m, "e")
	before := mm.(*Model).ta.Value()
	mm, _ = tuitest.Send(mm, tuitest.Resize(60, 20))
	if got := mm.(*Model).ta.Value(); got != before {
		t.Errorf("editor text changed by resize: %q -> %q", before, got)
	}
}
