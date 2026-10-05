package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/aeon022/diaryctl/internal/store"
)

// press builds the KeyPressMsg a real terminal would deliver for key, using
// bubbletea's own String() names ("enter", "esc", "ctrl+s", "space", "a", "ü").
func press(key string) tea.KeyPressMsg {
	switch key {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	}
	if rest, ok := strings.CutPrefix(key, "ctrl+"); ok {
		return tea.KeyPressMsg{Code: []rune(rest)[0], Mod: tea.ModCtrl}
	}
	r := []rune(key)
	return tea.KeyPressMsg{Code: r[0], Text: key}
}

// run feeds msg through Update and, like the bubbletea runtime, executes the
// returned command and feeds its message back — recursively (so the
// "delete → reload entries" chain completes), skipping timers and batches.
func run(t *testing.T, m *Model, msg tea.Msg) {
	t.Helper()
	_, cmd := m.Update(msg)
	drain(t, m, cmd, 0)
}

func drain(t *testing.T, m *Model, cmd tea.Cmd, depth int) {
	t.Helper()
	if cmd == nil || depth > 4 {
		return
	}
	// Timer commands (cursor blink, autosave/anim ticks) block for their delay
	// before returning; give a command a short window and drop it if it is a timer.
	out := make(chan tea.Msg, 1)
	go func() { out <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-out:
	case <-time.After(50 * time.Millisecond):
		return
	}
	switch msg.(type) {
	case nil, tea.QuitMsg, tea.BatchMsg, autoSaveTickMsg, animTickMsg:
	default:
		_, next := m.Update(msg)
		drain(t, m, next, depth+1)
	}
}

func keys(t *testing.T, m *Model, ks ...string) {
	t.Helper()
	for _, k := range ks {
		run(t, m, press(k))
	}
}

// newState builds a Model on a real temp store holding one entry per body
// (newest first, today backwards) and an isolated HOME.
func newState(t *testing.T, bodies ...string) (*Model, *store.Store, []time.Time) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DIARYCTL_DATA_DIR", "")
	s, err := store.Open(filepath.Join(t.TempDir(), "diary.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	var days []time.Time
	for i, b := range bodies {
		d := time.Now().AddDate(0, 0, -i)
		d = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
		if err := s.SaveEntry(d, b, false); err != nil {
			t.Fatal(err)
		}
		days = append(days, d)
	}
	m := New(s)
	m.width, m.height = 100, 30
	run(t, m, cmdLoadEntries(s)())
	return m, s, days
}

func TestEntriesLoadedClearsLoadingAndSetsEntries(t *testing.T) {
	m, _, _ := newState(t, "one", "two")
	if m.loading || len(m.entries) != 2 {
		t.Fatalf("loading=%v entries=%d", m.loading, len(m.entries))
	}
	if m.entries[0].Body != "one" {
		t.Errorf("entries should be newest first, got %q first", m.entries[0].Body)
	}
}

func TestListNavigationClampsAndDigitsJump(t *testing.T) {
	m, _, _ := newState(t, "a1", "b2", "c3")
	keys(t, m, "k", "up") // already at top
	if m.cursor != 0 {
		t.Errorf("cursor above top: %d", m.cursor)
	}
	keys(t, m, "j", "down", "j", "j", "down") // past the end
	if m.cursor != 2 {
		t.Errorf("cursor must clamp at last entry, got %d", m.cursor)
	}
	keys(t, m, "1")
	if m.cursor != 0 {
		t.Errorf("'1' should jump to the first visible entry, got %d", m.cursor)
	}
	keys(t, m, "3")
	if m.cursor != 2 {
		t.Errorf("'3' should jump to the third entry, got %d", m.cursor)
	}
	keys(t, m, "9") // no 9th entry — must be ignored, not panic
	if m.cursor != 2 {
		t.Errorf("'9' beyond the list moved the cursor to %d", m.cursor)
	}
}

func TestSearchFiltersEscClearsEnterKeeps(t *testing.T) {
	m, _, _ := newState(t, "coffee with anna", "gym day", "coffee alone")
	keys(t, m, "/", "c", "o", "f", "f", "e", "e")
	if !m.searching || len(m.visibleEntries()) != 2 {
		t.Fatalf("searching=%v visible=%d, want 2 'coffee' entries", m.searching, len(m.visibleEntries()))
	}
	keys(t, m, "enter")
	if m.searching || len(m.visibleEntries()) != 2 {
		t.Errorf("enter should leave search mode but keep the filter (searching=%v, visible=%d)", m.searching, len(m.visibleEntries()))
	}
	keys(t, m, "/", "esc")
	if m.searchQuery != "" || len(m.visibleEntries()) != 3 {
		t.Errorf("esc must clear the filter: query=%q visible=%d", m.searchQuery, len(m.visibleEntries()))
	}
}

// Regression: a v2 space press has String()=="space", and non-ASCII letters
// are multi-byte — a `len(msg.String()) == 1` check dropped both, so phrases
// ("coffee with") and German words (Müller) could not be typed into search.
func TestSearchAcceptsSpacesAndUmlauts(t *testing.T) {
	m, _, _ := newState(t, "coffee with anna", "Treffen mit Müller", "gym")
	keys(t, m, "/", "c", "o", "f", "f", "e", "e", "space", "w", "i", "t", "h")
	if m.searchQuery != "coffee with" {
		t.Fatalf("searchQuery = %q, want %q (space dropped?)", m.searchQuery, "coffee with")
	}
	if v := m.visibleEntries(); len(v) != 1 || v[0].Body != "coffee with anna" {
		t.Errorf("phrase search matched %+v", v)
	}
	keys(t, m, "esc", "/", "m", "ü", "l")
	if m.searchQuery != "mül" {
		t.Fatalf("searchQuery = %q, want %q (umlaut dropped?)", m.searchQuery, "mül")
	}
	keys(t, m, "backspace")
	if m.searchQuery != "mü" {
		t.Errorf("backspace after a multi-byte rune left %q, want %q (must remove a whole rune)", m.searchQuery, "mü")
	}
}

func TestPaletteNavigationAndNoMatch(t *testing.T) {
	m, _, _ := newState(t, "x")
	keys(t, m, ":")
	if !m.inPalette {
		t.Fatal("':' should open the palette")
	}
	keys(t, m, "down", "down", "down", "up", "up", "up", "up")
	if m.paletteCursor < 0 {
		t.Errorf("palette cursor underflow: %d", m.paletteCursor)
	}
	keys(t, m, "z", "z", "z", "z", "z", "enter") // matches nothing → closes quietly
	if m.inPalette || m.view != listView {
		t.Errorf("enter on an empty match list: inPalette=%v view=%v", m.inPalette, m.view)
	}
}

func TestDetailOpenScrollAndClose(t *testing.T) {
	m, _, _ := newState(t, strings.Repeat("line of text\n\n", 60))
	keys(t, m, "enter")
	if m.view != detailView || m.detail == nil {
		t.Fatalf("enter should open detail, view=%v", m.view)
	}
	keys(t, m, "j", "j", "down")
	if m.detailVP.YOffset() == 0 {
		t.Error("j/down should scroll the detail viewport")
	}
	keys(t, m, "esc")
	if m.view != listView || m.detail != nil {
		t.Errorf("esc should return to the list (view=%v detail=%v)", m.view, m.detail)
	}
}

func TestDeleteConfirmCancelAndConfirmWithUndo(t *testing.T) {
	m, s, days := newState(t, "keep me", "delete me")
	keys(t, m, "j", "d")
	if !m.confirmDelete {
		t.Fatal("'d' should ask for confirmation")
	}
	keys(t, m, "n")
	if m.confirmDelete || len(m.entries) != 2 {
		t.Errorf("any key but y cancels: confirm=%v entries=%d", m.confirmDelete, len(m.entries))
	}

	keys(t, m, "d", "y")
	if e, _ := s.GetEntry(days[1]); e != nil && e.Body != "" {
		t.Errorf("entry still in store after confirm: %+v", e)
	}
	if len(m.entries) != 1 || m.entries[0].Body != "keep me" {
		t.Errorf("list not reloaded after delete: %+v", m.entries)
	}
	if !strings.Contains(m.message, "undo") {
		t.Errorf("flash should mention undo, got %q", m.message)
	}

	keys(t, m, "u") // within undoWindow → restored
	if len(m.entries) != 2 {
		t.Fatalf("undo should restore the entry, entries=%d", len(m.entries))
	}
	if e, _ := s.GetEntry(days[1]); e == nil || e.Body != "delete me" {
		t.Errorf("restored entry = %+v", e)
	}
}

func TestUndoExpiresAfterWindow(t *testing.T) {
	m, _, _ := newState(t, "one", "two")
	keys(t, m, "d", "y")
	m.msgAt = time.Now().Add(-2 * undoWindow)
	keys(t, m, "u")
	if len(m.entries) != 1 {
		t.Errorf("undo after the window must do nothing, entries=%d", len(m.entries))
	}
}

func TestDetailDeleteGoesThroughConfirm(t *testing.T) {
	m, _, _ := newState(t, "solo")
	keys(t, m, "enter", "d")
	if m.view != listView || !m.confirmDelete || m.detail != nil {
		t.Errorf("'d' in detail: view=%v confirm=%v detail=%v — must return to the list and ask", m.view, m.confirmDelete, m.detail)
	}
}

func TestEditorSaveAndEscape(t *testing.T) {
	m, s, days := newState(t, "original")
	home := os.Getenv("HOME")
	cfg := filepath.Join(home, ".config", "notectl")
	_ = os.MkdirAll(cfg, 0o755)
	_ = os.WriteFile(filepath.Join(cfg, "notectl.yaml"), []byte("vault_path: ~/vault\n"), 0o644)

	keys(t, m, "e")
	if m.view != editorView || m.ta.Value() != "original" || m.editorDirty {
		t.Fatalf("open editor: view=%v value=%q dirty=%v", m.view, m.ta.Value(), m.editorDirty)
	}

	keys(t, m, "!") // a normal printable key goes to the textarea and dirties it
	if !m.editorDirty || !strings.Contains(m.ta.Value(), "!") {
		t.Errorf("typing: dirty=%v value=%q", m.editorDirty, m.ta.Value())
	}
	keys(t, m, "ctrl+s")
	if e, _ := s.GetEntry(days[0]); e == nil || !strings.Contains(e.Body, "!") || m.editorDirty {
		t.Errorf("ctrl+s: stored=%+v dirty=%v", e, m.editorDirty)
	}
	if b, err := os.ReadFile(filepath.Join(home, "vault", "Diary", days[0].Format("2006-01-02")+".md")); err != nil || !strings.Contains(string(b), "!") {
		t.Errorf("save must write back to the notectl vault: %q %v", b, err)
	}

	keys(t, m, "?") // dirty again, then esc saves + leaves
	keys(t, m, "esc")
	if m.view != listView || m.editorEntry != nil {
		t.Errorf("esc: view=%v editorEntry=%v", m.view, m.editorEntry)
	}
	if e, _ := s.GetEntry(days[0]); e == nil || !strings.Contains(e.Body, "?") {
		t.Errorf("esc with unsaved changes must save, stored=%+v", e)
	}
}

// Regression: the editor swallowed the letter "a" as the "ask AI" shortcut, so
// no text containing an 'a' could be typed (every German/English sentence).
// The AI trigger is ctrl+g now; plain 'a' must reach the textarea.
func TestEditorTypesLetterA(t *testing.T) {
	m, _, _ := newState(t, "")
	keys(t, m, "e", "a", "b", "a")
	if got := m.ta.Value(); got != "aba" {
		t.Errorf("textarea = %q, want %q", got, "aba")
	}
	if m.aiGenerating {
		t.Error("typing 'a' started AI generation")
	}
}

func TestEditorVimNormalModeSwallowsAndSavesOnEsc(t *testing.T) {
	m, s, days := newState(t, "text")
	keys(t, m, "e", "ctrl+v")
	if !m.vimNormal {
		t.Fatal("ctrl+v should enter vim normal mode")
	}
	keys(t, m, "x", "z") // other keys are swallowed in normal mode
	if m.ta.Value() != "text" || m.editorDirty {
		t.Errorf("normal mode must not edit: %q dirty=%v", m.ta.Value(), m.editorDirty)
	}
	keys(t, m, "i")
	if m.vimNormal {
		t.Error("'i' should return to insert mode")
	}
	keys(t, m, "!", "ctrl+v", "esc")
	if m.view != listView || m.vimNormal {
		t.Errorf("esc from normal mode: view=%v vimNormal=%v", m.view, m.vimNormal)
	}
	if e, _ := s.GetEntry(days[0]); e == nil || !strings.Contains(e.Body, "!") {
		t.Errorf("dirty entry must be saved on esc, stored=%+v", e)
	}
}

func TestHelpOverlayToggle(t *testing.T) {
	m, _, _ := newState(t, "x")
	keys(t, m, "?")
	if m.view != helpView {
		t.Fatalf("'?' should open help, view=%v", m.view)
	}
	keys(t, m, "?")
	if m.view != listView {
		t.Errorf("'?' again should close help, view=%v", m.view)
	}
}

func TestRepoViewDeleteConfirmAndUndo(t *testing.T) {
	m, s, _ := newState(t, "x")
	if err := s.SaveRepo("/tmp/repo-a", "repo-a"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRepo("/tmp/repo-b", "repo-b"); err != nil {
		t.Fatal(err)
	}
	run(t, m, cmdLoadRepos(s)())
	keys(t, m, "r")
	if m.view != repoView || len(m.repos) != 2 {
		t.Fatalf("view=%v repos=%d", m.view, len(m.repos))
	}
	keys(t, m, "d", "n")
	if m.confirmDeleteRepo || len(m.repos) != 2 {
		t.Errorf("cancel: confirm=%v repos=%d", m.confirmDeleteRepo, len(m.repos))
	}
	keys(t, m, "d", "y")
	if len(m.repos) != 1 {
		t.Fatalf("repo not deleted, repos=%d", len(m.repos))
	}
	keys(t, m, "u")
	if len(m.repos) != 2 {
		t.Errorf("undo should restore the repo, repos=%d", len(m.repos))
	}
	keys(t, m, "esc")
	if m.view != listView {
		t.Errorf("esc should leave the repo view, view=%v", m.view)
	}
}

func TestCtrlCQuitsFromEveryView(t *testing.T) {
	for name, setup := range map[string][]string{"list": nil, "detail": {"enter"}, "editor": {"e"}, "repos": {"r"}, "help": {"?"}} {
		t.Run(name, func(t *testing.T) {
			m, _, _ := newState(t, "x")
			keys(t, m, setup...)
			_, cmd := m.Update(press("ctrl+c"))
			if cmd == nil {
				t.Fatal("ctrl+c returned no command")
			}
			if _, ok := cmd().(tea.QuitMsg); !ok {
				t.Error("ctrl+c did not quit")
			}
		})
	}
}

func TestWindowSizeAndMouse(t *testing.T) {
	m, _, _ := newState(t, "a", "b", "c")
	run(t, m, tea.WindowSizeMsg{Width: 120, Height: 0})
	if m.width != 120 || m.height != 1 {
		t.Errorf("size = %dx%d, want 120x1 (height never below 1)", m.width, m.height)
	}
	run(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	run(t, m, tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	run(t, m, tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	run(t, m, tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if m.cursor != 2 {
		t.Errorf("wheel down clamps at the last entry, cursor=%d", m.cursor)
	}
	run(t, m, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if m.cursor != 1 {
		t.Errorf("wheel up: cursor=%d", m.cursor)
	}

	// locate row 0 on screen the same way the app does, then double-click it
	x, y := heatmapPanelW+4, -1
	for yy := 0; yy < 40 && y < 0; yy++ {
		if m.rowHitTest(x, yy) == 0 {
			y = yy
		}
	}
	if y < 0 {
		t.Fatal("no screen row maps to entry 0")
	}
	click := tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: y}
	run(t, m, click)
	if m.cursor != 0 || m.view != listView {
		t.Errorf("single click selects only: cursor=%d view=%v", m.cursor, m.view)
	}
	run(t, m, click)
	if m.view != detailView {
		t.Errorf("double click should open the entry, view=%v", m.view)
	}
}
