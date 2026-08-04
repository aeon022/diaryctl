package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/aeon022/diaryctl/internal/models"
	"github.com/aeon022/missionctl-core/palette"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestLoadingSpinner_ShowsWhileLoadingThenEmptyStateOnceDone(t *testing.T) {
	m := &Model{width: 100, height: 30, loading: true, sp: spinner.New()}

	view := m.renderEntryList(60, 20)
	if !strings.Contains(view, "Loading entries") {
		t.Errorf("expected loading spinner text while m.loading is true, got:\n%s", view)
	}
	if strings.Contains(view, "No entries yet") {
		t.Error("expected the empty-state message to be suppressed while loading")
	}

	// entriesLoadedMsg's handler sets loading = false (also touches m.store,
	// which needs a real *store.Store — out of scope for this render test,
	// so the field flip is asserted directly rather than via Update).
	m.loading = false

	view = m.renderEntryList(60, 20)
	if !strings.Contains(view, "No entries yet") {
		t.Errorf("expected the empty-state message once loading is done and there are no entries, got:\n%s", view)
	}
}

func TestCommandPalette_TypeFilterAndExecute(t *testing.T) {
	m := &Model{width: 100, height: 30}

	m.handleList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	if !m.inPalette {
		t.Fatal("expected inPalette after ':'")
	}

	for _, r := range "rep" {
		m.handleList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	matches := palette.Match(paletteCommands, m.paletteQuery)
	if len(matches) == 0 || matches[0].Name != "repos" {
		t.Fatalf("expected 'repos' to be the top match for query %q, got %v", m.paletteQuery, matches)
	}

	m.handleList(tea.KeyMsg{Type: tea.KeyEnter})
	if m.inPalette {
		t.Error("expected palette to close after executing a command")
	}
	if m.view != repoView {
		t.Errorf("expected 'repos' command to replay 'r' and open repoView, got %v", m.view)
	}
}

func TestCommandPalette_EscCloses(t *testing.T) {
	m := &Model{width: 100, height: 30}
	m.handleList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})

	m.handleList(tea.KeyMsg{Type: tea.KeyEsc})
	if m.inPalette {
		t.Error("expected esc to close the palette")
	}
}

// TestDetailScroll_ClampsAtEndOfContent is a regression test for a bug
// caught via live tmux testing: scrolling down in the detail view had no
// upper bound, so detailScrl could climb past the entry's actual line
// count. viewDetail's start/end math clamped `start` to len(bodyLines) but
// not to a position that still shows a full page, so a slice like
// bodyLines[len:len] rendered as an emptier and emptier panel the further
// past the end you scrolled.
func TestDetailScroll_ClampsAtEndOfContent(t *testing.T) {
	body := strings.Repeat("line\n", 5) // far fewer lines than any reasonable terminal height
	m := &Model{
		width:  100,
		height: 30,
		view:   detailView,
		detail: &models.Entry{Body: body},
	}

	for i := 0; i < 100; i++ {
		m.handleDetail(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	}

	want := m.detailMaxScroll()
	if m.detailScrl != want {
		t.Errorf("expected detailScrl to clamp at %d (detailMaxScroll), got %d", want, m.detailScrl)
	}

	view := m.viewDetail()
	if !strings.Contains(view, "line") {
		t.Errorf("expected the panel to still show real content after scrolling far past the end, got:\n%s", view)
	}
}

func TestHeatLevel_Thresholds(t *testing.T) {
	cases := map[int]int{0: 0, -1: 0, 1: 1, 2: 1, 3: 2, 5: 2, 6: 3, 9: 3, 10: 4, 100: 4}
	for count, want := range cases {
		if got := heatLevel(count); got != want {
			t.Errorf("heatLevel(%d) = %d, want %d", count, got, want)
		}
	}
}

func TestRenderHeatmap_FitsPanelWidthAndShowsLegend(t *testing.T) {
	m := &Model{width: 100, height: 30}
	out := m.renderHeatmap()
	if !strings.Contains(out, "10+") {
		t.Error("expected the heatmap to include its color legend")
	}
	for _, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > heatmapPanelW-4 {
			t.Errorf("heatmap line exceeds panel content width (%d): %q", heatmapPanelW-4, line)
		}
	}
}

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

func TestBodyFuzzyMatches_MatchesIndividualWords(t *testing.T) {
	if !bodyFuzzyMatches("had a big breakfast today", "brekfst") {
		t.Error("expected fuzzy 'brekfst' to match the word 'breakfast'")
	}
	if bodyFuzzyMatches("had a big breakfast today", "xyz") {
		t.Error("expected 'xyz' not to match")
	}
}

func TestBodyFuzzyMatches_DoesNotMatchAcrossWordsAsOneSubsequence(t *testing.T) {
	// Regression guard for the design decision: fuzzy must operate per-word,
	// not across the whole body as one subsequence — otherwise almost any
	// short query would find SOME subsequence in a full paragraph and
	// over-match everything, defeating the point of search.
	body := "a big beach trip yesterday"
	// "bibetr" is a subsequence of the WHOLE body ("bi[g] [beach tri]p")
	// but matches no single word.
	if bodyFuzzyMatches(body, "bibetr") {
		t.Error("expected fuzzy matching to be scoped per-word, not across the whole body")
	}
}

func TestFilterEntries_PreservesExistingPhraseSearch(t *testing.T) {
	// Regression guard: adding fuzzy must not break the pre-existing
	// multi-word substring/phrase search.
	m := &Model{
		entries: []models.Entry{
			{ID: 1, Body: "went to the beach with friends", Date: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)},
			{ID: 2, Body: "stayed home all day", Date: time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)},
		},
	}
	m.searchQuery = "the beach"
	m.filterEntries()
	if len(m.searchRes) != 1 || m.searchRes[0].ID != 1 {
		t.Errorf("expected the phrase 'the beach' to still match entry 1 via substring search, got %+v", m.searchRes)
	}
}

func TestFilterEntries_MatchesByDateSubstring(t *testing.T) {
	m := &Model{
		entries: []models.Entry{
			{ID: 1, Body: "unrelated", Date: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)},
			{ID: 2, Body: "unrelated", Date: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)},
		},
	}
	m.searchQuery = "2026-07"
	m.filterEntries()
	if len(m.searchRes) != 1 || m.searchRes[0].ID != 1 {
		t.Errorf("expected date substring '2026-07' to match only entry 1, got %+v", m.searchRes)
	}
}
