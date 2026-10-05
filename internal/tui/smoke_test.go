package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/aeon022/missionctl-core/tuitest"
)

// every view/mode reachable from the entry list, each left via esc
var smokeFlows = [][]string{
	{"j", "k", "enter", "j", "k", "esc"},
	{"e", "h", "j", "k", "i", "x", "y", "esc", "esc"},
	{"/", "a", "b", "esc"},
	{":", "esc"},
	{"?", "j", "k", "esc"},
	{"r", "j", "esc"},
	{"d", "esc"},
	{"u", "n", "esc"},
	{"1", "2", "3"},
}

func TestSmokeAllViews(t *testing.T) {
	for _, bodies := range [][]string{{"first entry", "second entry\nmore text"}, nil} {
		for _, f := range smokeFlows {
			t.Run(strings.Join(f, " "), func(t *testing.T) {
				m, _, _ := newState(t, bodies...)
				tuitest.Smoke(t, m, f...)
			})
		}
	}
}

func TestSmokeSmallTerminal(t *testing.T) {
	for _, f := range smokeFlows {
		m, _, _ := newState(t, "first entry", "second entry")
		mm, _ := tuitest.Send(m, tuitest.Resize(60, 15))
		mm, _ = tuitest.Keys(mm, f...)
		if strings.TrimSpace(tuitest.Text(mm)) == "" {
			t.Errorf("empty view at 60x15 after %v", f)
		}
	}
}

// footers (detail + repos) must never be wider than the terminal
func TestFootersFitWidth(t *testing.T) {
	for _, w := range []int{50, 60, 80, 100} {
		m, _, _ := newState(t, "first entry")
		mm, _ := tuitest.Send(m, tuitest.Resize(w, 20))
		mm, _ = tuitest.Keys(mm, "enter")
		detail := strings.Split(tuitest.Text(mm), "\n")
		if last := detail[len(detail)-1]; lipgloss.Width(last) > w || !strings.Contains(last, "esc back") {
			t.Errorf("detail footer at width %d: %q", w, last)
		}
		mm, _ = tuitest.Keys(mm, "esc", "r")
		repos := strings.Split(tuitest.Text(mm), "\n")
		if last := repos[len(repos)-1]; lipgloss.Width(last) > w || !strings.Contains(last, "esc back") {
			t.Errorf("repos footer at width %d: %q", w, last)
		}
	}
}
