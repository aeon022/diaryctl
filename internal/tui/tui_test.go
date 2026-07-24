package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/aeon022/diaryctl/internal/models"
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
