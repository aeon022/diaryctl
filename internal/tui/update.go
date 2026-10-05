package tui

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/aeon022/diaryctl/internal/ai"
	"github.com/aeon022/diaryctl/internal/diary"
	"github.com/aeon022/diaryctl/internal/git"
	"github.com/aeon022/diaryctl/internal/models"
	"github.com/aeon022/diaryctl/internal/notectl"
	"github.com/aeon022/diaryctl/internal/store"
	"github.com/aeon022/diaryctl/internal/suite"
	"github.com/aeon022/missionctl-core/palette"
	"github.com/sahilm/fuzzy"
)

// ── Init ──────────────────────────────────────────────────────────────────────

func (m *Model) Init() tea.Cmd {
	return tea.Batch(cmdLoadEntries(m.store), cmdLoadRepos(m.store), cmdAnimTick(), m.sp.Tick)
}

// cmdRestoreEntry re-saves a deleted entry with its original date/body —
// used by "u" within undoWindow of a delete. SaveEntry is an upsert keyed
// by date, so this recreates the exact same entry.
func cmdRestoreEntry(s *store.Store, e models.Entry) tea.Cmd {
	return func() tea.Msg {
		if err := s.SaveEntry(e.Date, e.Body, e.Generated); err != nil {
			return errMsg{err}
		}
		entries, err := s.ListEntries(100)
		if err != nil {
			return errMsg{err}
		}
		return entriesLoadedMsg{entries}
	}
}

func cmdLoadEntries(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		entries, err := s.ListEntries(100)
		if err != nil {
			return errMsg{err}
		}
		return entriesLoadedMsg{entries}
	}
}

// cmdRestoreRepo re-saves a deleted repo with its original path/name —
// used by "u" within undoWindow of a delete. SaveRepo is an upsert keyed
// by path, so this recreates the exact same registration.
func cmdRestoreRepo(s *store.Store, r models.Repo) tea.Cmd {
	return func() tea.Msg {
		if err := s.SaveRepo(r.Path, r.Name); err != nil {
			return errMsg{err}
		}
		repos, err := s.ListRepos()
		if err != nil {
			return errMsg{err}
		}
		return reposLoadedMsg{repos}
	}
}

func cmdLoadRepos(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		repos, err := s.ListRepos()
		if err != nil {
			return errMsg{err}
		}
		return reposLoadedMsg{repos}
	}
}

// cmdLoadTodaySummary reads git commits + suite data for today asynchronously.
func cmdLoadTodaySummary(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		repos, err := s.ListRepos()
		if err != nil {
			return todaySummaryMsg{}
		}
		today := time.Now()
		ds, _ := git.DayStats(repos, today)
		tasks, _ := suite.TodayTasks()
		events, _ := suite.TodayEvents()
		times, _ := suite.TodayTimeEntries()
		return todaySummaryMsg{
			commits:  len(ds.Commits),
			tasks:    len(tasks),
			events:   len(events),
			duration: suite.TotalDuration(times),
		}
	}
}

// copyToClipboardCmd copies text two ways: OSC 52 (tea.SetClipboard — works
// over SSH/tmux in terminals that allow it) and pbcopy (Terminal.app and
// anything that ignores OSC 52). Same pbcopy approach as taskctl/mailctl/
// notectl/calctl/habctl/timectl use for their "y" shortcuts.
func copyToClipboardCmd(text string) tea.Cmd {
	return tea.Batch(tea.SetClipboard(text), func() tea.Msg {
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(text)
		_ = cmd.Run()
		return nil
	})
}

// jumpToNoteCmd shells out to `notectl --open <path>` to jump straight to
// the vault note for date, matching the "Diary/<date>.md" path notectl.
// WriteBack (internal/notectl/writeback.go) uses. If notectl isn't
// installed or the note was never written back, notectl just opens on its
// normal list — same graceful-degradation as WriteBack's own silent skip.
func jumpToNoteCmd(date time.Time) tea.Cmd {
	relPath := "Diary/" + date.Format("2006-01-02") + ".md"
	return tea.ExecProcess(exec.Command("notectl", "--open", relPath), func(err error) tea.Msg {
		if err != nil {
			return errMsg{err}
		}
		return nil
	})
}

func cmdGenerateToday(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		repos, err := s.ListRepos()
		if err != nil {
			return entryGenMsg{err: err}
		}
		today := time.Now()

		ds, err := git.DayStats(repos, today)
		if err != nil {
			return entryGenMsg{err: err}
		}
		byRepo, _ := git.CommitsByRepo(repos, today)
		ds.ByRepo = byRepo
		streak, _ := s.GetStreak()
		ds.Streak = streak

		tasks, _ := suite.TodayTasks()
		events, _ := suite.TodayEvents()
		times, _ := suite.TodayTimeEntries()
		habits, _ := suite.TodayHabits()

		body := diary.BuildEntryBody(ds, tasks, events, times, habits)
		if err := s.SaveEntry(today, body, false); err != nil {
			return entryGenMsg{err: err}
		}
		_ = notectl.WriteBack(today, body)
		entry, _ := s.GetEntry(today)
		return entryGenMsg{entry: entry}
	}
}

func cmdAutoSaveTick() tea.Cmd {
	return tea.Tick(30*time.Second, func(time.Time) tea.Msg {
		return autoSaveTickMsg{}
	})
}

func cmdAnimTick() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg {
		return animTickMsg{}
	})
}

func waitForAI(ch chan ai.StreamResult) tea.Cmd {
	return func() tea.Msg {
		r := <-ch
		switch {
		case r.Err != nil:
			return aiErrMsg{r.Err}
		case r.Done:
			return aiDoneMsg{r.Chunk}
		default:
			return aiChunkMsg{r.Chunk}
		}
	}
}

// ── Update ────────────────────────────────────────────────────────────────────

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.FocusMsg:
		// Back from another window: entries may have changed (a note written
		// by another tool or machine on a shared data_dir). Reload — but only
		// from the plain list, never from the editor/detail/search/palette/
		// confirm states, so no unsaved text or typed query is ever
		// disturbed, and at most once per 5s.
		if m.view == listView && !m.searching && !m.inPalette && !m.confirmDelete &&
			!m.loading && time.Since(m.lastLoad) > 5*time.Second {
			m.lastLoad = time.Now()
			return m, cmdLoadEntries(m.store)
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		// -1: reserves one row of slack so View() output never has exactly
		// as many lines as the real terminal height — combined with no
		// trailing newline, that exact match can make bubbletea v1 fail to
		// fully redraw (charmbracelet/bubbletea#304).
		m.height = msg.Height - 1
		if m.height < 1 {
			m.height = 1
		}
		if m.view == editorView {
			m.resizeEditor()
		}
		if m.view == detailView {
			m.resizeDetailVP()
		}
		return m, nil

	case entriesLoadedMsg:
		m.entries = msg.entries
		m.lastLoad = time.Now()
		if m.cursor >= len(m.entries) { // entries can shrink under us (focus reload)
			m.cursor = max(0, len(m.entries)-1)
		}
		m.streak, _ = m.store.GetStreak()
		m.loading = false
		return m, nil

	case spinner.TickMsg:
		if m.loading {
			var cmd tea.Cmd
			m.sp, cmd = m.sp.Update(msg)
			return m, cmd
		}
		return m, nil

	case reposLoadedMsg:
		m.repos = msg.repos
		return m, cmdLoadTodaySummary(m.store)

	case todaySummaryMsg:
		m.todayCommits = msg.commits
		m.todayTasks = msg.tasks
		m.todayEvents = msg.events
		m.todayDuration = msg.duration
		m.todayLoaded = true
		return m, nil

	case animTickMsg:
		m.tickCount++
		return m, cmdAnimTick()

	case entryGenMsg:
		if msg.err != nil {
			m.flash("Error: " + msg.err.Error())
		} else {
			m.flash("Entry generated — press e to edit")
		}
		return m, cmdLoadEntries(m.store)

	case autoSaveTickMsg:
		if m.view == editorView && m.editorDirty {
			m.save()
		}
		return m, cmdAutoSaveTick()

	case aiChunkMsg:
		m.aiTokens += len(strings.Fields(msg.chunk))
		return m, waitForAI(m.aiChan)

	case aiDoneMsg:
		m.aiGenerating = false
		if msg.full != "" {
			before := diary.WordCount(m.aiBeforeContent)
			after := diary.WordCount(msg.full)
			m.ta.SetValue(msg.full)
			m.editorDirty = true
			m.flash(fmt.Sprintf("AI wrote %d words (+%d) — review and ctrl+s to save", after, after-before))
		} else {
			m.flash("AI finished — review and ctrl+s to save")
		}
		return m, nil

	case aiErrMsg:
		m.aiGenerating = false
		m.flash("AI error: " + msg.err.Error())
		return m, nil

	case errMsg:
		m.err = msg.err
		m.loading = false
		return m, nil

	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			if m.view == listView && m.cursor > 0 {
				m.cursor--
			}
		case tea.MouseWheelDown:
			if m.view == listView && m.cursor < len(m.visibleEntries())-1 {
				m.cursor++
			}
		}
		return m, nil

	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft || m.view != listView {
			return m, nil
		}
		if i := m.rowHitTest(msg.X, msg.Y); i >= 0 {
			now := time.Now()
			if i == m.lastClickRow && now.Sub(m.lastClickAt) < doubleClickWindow {
				m.cursor = i
				m.lastClickRow = -1 // consumed, so a third click starts fresh
				e := m.visibleEntries()[i]
				m.openDetail(&e)
				return m, nil
			}
			m.cursor = i
			m.lastClickRow = i
			m.lastClickAt = now
		}
		return m, nil

	case tea.MouseMotionMsg:
		if m.view == listView {
			m.hoverRow = m.rowHitTest(msg.X, msg.Y)
		}
		return m, nil

	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		switch m.view {
		case listView:
			return m, m.handleList(msg)
		case detailView:
			return m, m.handleDetail(msg)
		case editorView:
			return m, m.handleEditor(msg)
		case repoView:
			return m, m.handleRepo(msg)
		case helpView:
			return m, m.handleHelp(msg)
		}
		return m, nil
	}

	if m.view == editorView {
		var cmd tea.Cmd
		m.ta, cmd = m.ta.Update(msg)
		return m, cmd
	}

	return m, nil
}

// ── Key handlers ─────────────────────────────────────────────────────────────

func (m *Model) handleList(msg tea.KeyPressMsg) tea.Cmd {
	if m.confirmDelete {
		if msg.String() == "y" || msg.String() == "Y" {
			_ = m.store.DeleteEntry(m.deleteDate)
			m.lastDeleted = m.deleteTarget
			m.deleteTarget = nil
			m.flash(fmt.Sprintf("Deleted %s — press u to undo", m.deleteDate.Format("2006-01-02")))
			if m.cursor > 0 {
				m.cursor--
			}
			m.confirmDelete = false
			return cmdLoadEntries(m.store)
		}
		m.confirmDelete = false
		m.deleteTarget = nil
		return nil
	}

	if m.inPalette {
		switch msg.String() {
		case "esc":
			m.inPalette = false
			m.paletteQuery = ""
			m.paletteCursor = 0
		case "up", "ctrl+p":
			if m.paletteCursor > 0 {
				m.paletteCursor--
			}
		case "down", "ctrl+n":
			matches := palette.Match(paletteCommands, m.paletteQuery)
			if m.paletteCursor < len(matches)-1 {
				m.paletteCursor++
			}
		case "enter":
			matches := palette.Match(paletteCommands, m.paletteQuery)
			m.inPalette = false
			m.paletteQuery = ""
			if len(matches) == 0 {
				m.paletteCursor = 0
				return nil
			}
			if m.paletteCursor >= len(matches) {
				m.paletteCursor = len(matches) - 1
			}
			chosen := matches[m.paletteCursor]
			m.paletteCursor = 0
			replay := tea.KeyPressMsg{Text: chosen.Key, Code: []rune(chosen.Key)[0]}
			if chosen.Key == "enter" {
				replay = tea.KeyPressMsg{Code: tea.KeyEnter}
			}
			return m.handleList(replay)
		case "backspace":
			m.paletteQuery = dropLastRune(m.paletteQuery)
		default:
			m.paletteQuery += typedText(msg)
		}
		return nil
	}

	if m.searching {
		switch msg.String() {
		case "esc":
			m.searching = false
			m.searchQuery = ""
			m.searchRes = nil
		case "enter":
			m.searching = false
		case "backspace":
			if m.searchQuery != "" {
				m.searchQuery = dropLastRune(m.searchQuery)
				m.filterEntries()
			}
		default:
			if t := typedText(msg); t != "" {
				m.searchQuery += t
				m.filterEntries()
			}
		}
		return nil
	}

	entries := m.visibleEntries()
	switch msg.String() {
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		// jump to the nth visible (on-screen) entry — mirrors rowHitTest's
		// own scroll-window math (maxVis/start) so a digit lands on the
		// same entry a click at that position would.
		n := int(msg.String()[0] - '0')
		maxVis := m.panelHeight() - 3
		start := 0
		if m.cursor >= maxVis {
			start = m.cursor - maxVis + 1
		}
		if idx := start + n - 1; idx < len(entries) && n <= maxVis {
			m.cursor = idx
		}
	case "j", "down":
		if m.cursor < len(entries)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "enter":
		if len(entries) > 0 {
			e := entries[m.cursor]
			m.openDetail(&e)
		}
	case "e":
		if len(entries) > 0 {
			e := entries[m.cursor]
			return m.openEditor(&e)
		}
	case "y":
		if len(entries) > 0 {
			m.flash("Copied to clipboard")
			return copyToClipboardCmd(entries[m.cursor].Date.Format("2006-01-02"))
		}
	case "g":
		if len(entries) > 0 {
			return jumpToNoteCmd(entries[m.cursor].Date)
		}
	case "u":
		if m.lastDeleted != nil && time.Since(m.msgAt) < undoWindow {
			e := m.lastDeleted
			m.lastDeleted = nil
			m.message = ""
			return cmdRestoreEntry(m.store, *e)
		}
	case "n":
		m.flash("Generating today's entry…")
		return cmdGenerateToday(m.store)
	case "d":
		if len(entries) > 0 {
			e := entries[m.cursor]
			m.confirmDelete = true
			m.deleteDate = e.Date
			m.deleteTarget = &e
		}
	case "r":
		m.view = repoView
		m.repoCursor = 0
	case ":":
		m.inPalette = true
		m.paletteQuery = ""
		m.paletteCursor = 0
	case "/":
		m.searching = true
		m.searchQuery = ""
	case "?":
		m.openHelp()
	case "q":
		return tea.Quit
	}
	return nil
}

func (m *Model) handleHelp(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "q", "esc", "?":
		m.view = listView
		return nil
	}
	var cmd tea.Cmd
	m.helpVP, cmd = m.helpVP.Update(msg)
	return cmd
}

// openDetail opens an entry in the detail view, sizing and populating
// detailVP from scratch (see resizeDetailVP).
func (m *Model) openDetail(entry *models.Entry) {
	m.detail = entry
	m.detailVP = viewport.New()
	m.resizeDetailVP()
	m.detailVP.GotoTop()
	m.view = detailView
}

// resizeDetailVP re-wraps the current entry's body to the terminal's
// current width and resizes the viewport accordingly — called on open and
// on every window resize. Pre-wrapping (rather than letting the viewport
// window raw, unwrapped lines) is what keeps the header/footer fixed for
// long entries: the previous implementation paginated by raw line count,
// which undercounted any line long enough for lipgloss to wrap into
// several visual rows, so the rendered body grew taller than its budget
// and the whole screen — not just the body — ended up scrolling, taking
// the header off the top with it.
func (m *Model) resizeDetailVP() {
	if m.detail == nil {
		return
	}
	w, h := m.width, m.height
	if w < 40 {
		w = 80
	}
	if h < 20 {
		h = 24
	}
	innerW := w - 6 // mirrors panelStyle's border(2)+padding(2) below
	if innerW < 10 {
		innerW = 10
	}
	vpH := h - 6
	if vpH < 3 {
		vpH = 3
	}
	m.detailVP.SetWidth(innerW)
	m.detailVP.SetHeight(vpH)
	m.detailVP.SetContent(lipgloss.NewStyle().Width(innerW).Render(renderMarkdown(m.detail.Body)))
}

func (m *Model) handleDetail(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "q":
		m.view = listView
		m.detail = nil
	case "j", "down":
		m.detailVP.ScrollDown(1)
	case "k", "up":
		m.detailVP.ScrollUp(1)
	case "e":
		if m.detail != nil {
			return m.openEditor(m.detail)
		}
	case "g":
		if m.detail != nil {
			return jumpToNoteCmd(m.detail.Date)
		}
	case "d":
		if m.detail != nil {
			m.confirmDelete = true
			m.deleteDate = m.detail.Date
			m.deleteTarget = m.detail
			m.view = listView
			m.detail = nil
		}
	}
	return nil
}

// startAI streams an AI continuation of the open entry into the editor. It is
// bound to ctrl+g (works in insert and vim-normal mode): a plain letter can't
// be the trigger in a text editor, or that letter could never be typed.
func (m *Model) startAI() tea.Cmd {
	if m.aiGenerating {
		return nil
	}
	m.aiGenerating = true
	m.aiTokens = 0
	m.aiBeforeContent = m.ta.Value()
	m.aiChan = make(chan ai.StreamResult, 64)
	body := m.ta.Value()
	ch := m.aiChan
	return tea.Batch(
		func() tea.Msg {
			go ai.Stream(body, ch)
			return nil
		},
		waitForAI(ch),
	)
}

// typedText is the printable text of a key press ("" for ctrl/alt chords and
// named keys). Unlike len(msg.String()) == 1 it accepts space (String() is
// "space" in Bubble Tea v2) and multi-byte letters such as ü.
func typedText(msg tea.KeyPressMsg) string {
	if msg.Mod&(tea.ModCtrl|tea.ModAlt) != 0 {
		return ""
	}
	return msg.Text
}

// dropLastRune removes the final rune (not byte) so backspace can't leave a
// half-encoded character behind.
func dropLastRune(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return string(r[:len(r)-1])
}

func (m *Model) handleEditor(msg tea.KeyPressMsg) tea.Cmd {
	if msg.String() == "ctrl+g" {
		return m.startAI()
	}
	// Vim normal mode — intercept keys before passing to textarea.
	if m.vimNormal {
		switch msg.String() {
		case "i", "a", "A", "o", "O":
			m.vimNormal = false
		case "h":
			var cmd tea.Cmd
			m.ta, cmd = m.ta.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
			return cmd
		case "l":
			var cmd tea.Cmd
			m.ta, cmd = m.ta.Update(tea.KeyPressMsg{Code: tea.KeyRight})
			return cmd
		case "j":
			var cmd tea.Cmd
			m.ta, cmd = m.ta.Update(tea.KeyPressMsg{Code: tea.KeyDown})
			return cmd
		case "k":
			var cmd tea.Cmd
			m.ta, cmd = m.ta.Update(tea.KeyPressMsg{Code: tea.KeyUp})
			return cmd
		case "0":
			var cmd tea.Cmd
			m.ta, cmd = m.ta.Update(tea.KeyPressMsg{Code: tea.KeyHome})
			return cmd
		case "$":
			var cmd tea.Cmd
			m.ta, cmd = m.ta.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
			return cmd
		case "ctrl+s":
			m.save()
			return cmdLoadEntries(m.store)
		case "esc":
			if m.editorDirty {
				m.save()
			}
			m.view = listView
			m.editorEntry = nil
			m.vimNormal = false
			m.ta.Blur()
			return cmdLoadEntries(m.store)
		default:
			return nil // swallow all other keys in normal mode
		}
		return nil
	}

	switch msg.String() {
	case "ctrl+s":
		m.save()
		return cmdLoadEntries(m.store)

	case "esc":
		if m.editorDirty {
			m.save()
		}
		m.view = listView
		m.editorEntry = nil
		m.ta.Blur()
		return cmdLoadEntries(m.store)

	case "ctrl+v":
		m.vimNormal = true
		return nil

	case "ctrl+f":
		m.centeredMode = !m.centeredMode
		m.resizeEditor()
		return nil

	case "tab":
		content := m.ta.Value()
		line := m.ta.Line()
		pos := lineToOffset(content, line)
		if next := strings.Index(content[pos+1:], "<!-- AI:"); next >= 0 {
			_ = offsetToLine(content, pos+1+next)
		}
		var cmd tea.Cmd
		m.ta, cmd = m.ta.Update(msg)
		return cmd

	case "[":
		content := m.ta.Value()
		line := m.ta.Line()
		pos := lineToOffset(content, line)
		if prev := strings.LastIndex(content[:pos], "\n## "); prev >= 0 {
			_ = offsetToLine(content, prev+1)
		}
		return nil

	case "]":
		content := m.ta.Value()
		line := m.ta.Line()
		pos := lineToOffset(content, line)
		if next := strings.Index(content[pos+1:], "\n## "); next >= 0 {
			_ = offsetToLine(content, pos+1+next+1)
		}
		return nil
	}

	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	m.editorDirty = true
	return cmd
}

func (m *Model) handleRepo(msg tea.KeyPressMsg) tea.Cmd {
	if m.confirmDeleteRepo {
		if msg.String() == "y" || msg.String() == "Y" {
			r := m.repos[m.repoCursor]
			_ = m.store.DeleteRepo(r.Path)
			m.lastDeletedRepo = &r
			m.flash(fmt.Sprintf("Deleted %s — press u to undo", r.Name))
			m.confirmDeleteRepo = false
			if m.repoCursor > 0 {
				m.repoCursor--
			}
			return cmdLoadRepos(m.store)
		}
		m.confirmDeleteRepo = false
		return nil
	}

	switch msg.String() {
	case "esc", "q":
		m.view = listView
	case "j", "down":
		if m.repoCursor < len(m.repos)-1 {
			m.repoCursor++
		}
	case "k", "up":
		if m.repoCursor > 0 {
			m.repoCursor--
		}
	case "d":
		if len(m.repos) > 0 {
			m.confirmDeleteRepo = true
		}
	case "u":
		if m.lastDeletedRepo != nil && time.Since(m.msgAt) < undoWindow {
			r := m.lastDeletedRepo
			m.lastDeletedRepo = nil
			m.message = ""
			return cmdRestoreRepo(m.store, *r)
		}
	}
	return nil
}

// ── Editor helpers ────────────────────────────────────────────────────────────

func (m *Model) openEditor(entry *models.Entry) tea.Cmd {
	m.editorEntry = entry
	m.ta.SetValue(entry.Body)
	m.editorDirty = false
	m.lastSaved = time.Now()
	m.savedFlash = false
	m.vimNormal = false
	m.view = editorView
	m.resizeEditor()
	return tea.Batch(cmdAutoSaveTick(), m.ta.Focus())
}

func (m *Model) save() {
	if m.editorEntry == nil {
		return
	}
	body := m.ta.Value()
	_ = m.store.SaveEntry(m.editorEntry.Date, body, m.editorEntry.Generated)
	_ = notectl.WriteBack(m.editorEntry.Date, body)
	m.editorDirty = false
	m.lastSaved = time.Now()
	m.savedFlash = true
}

func (m *Model) resizeEditor() {
	w, h := m.width, m.height
	if w < 40 {
		w = 80
	}
	if h < 20 {
		h = 24
	}
	if m.centeredMode {
		tw := 78
		if w < tw+6 {
			tw = w - 6
		}
		m.ta.SetWidth(tw)
	} else {
		m.ta.SetWidth(w - 6)
	}
	m.ta.SetHeight(h - 8)
}

func (m *Model) flash(s string) {
	m.message = s
	m.msgAt = time.Now()
}

func (m *Model) filterEntries() {
	if m.searchQuery == "" {
		m.searchRes = nil
		return
	}
	q := strings.ToLower(m.searchQuery)
	var res []models.Entry
	for _, e := range m.entries {
		body := strings.ToLower(e.Body)
		if strings.Contains(body, q) ||
			strings.Contains(e.Date.Format("2006-01-02"), q) ||
			bodyFuzzyMatches(body, q) {
			res = append(res, e)
		}
	}
	m.searchRes = res
	m.cursor = 0
}

// bodyFuzzyMatches reports whether q fuzzy-matches any individual WORD in
// body, added alongside (not instead of) the substring/phrase check above.
// Fuzzy-matching the whole body as one subsequence would be nearly
// meaningless for free-form journal text — almost any short query finds
// SOME subsequence across a full paragraph, over-matching everything and
// defeating the point of search. Matching per-word keeps fuzzy's typo/
// abbreviation tolerance (e.g. "brekfst" still finds "breakfast") without
// that over-matching, and without breaking multi-word phrase search, which
// the substring check above still handles.
func bodyFuzzyMatches(body, q string) bool {
	return len(fuzzy.Find(q, strings.Fields(body))) > 0
}

func (m *Model) visibleEntries() []models.Entry {
	if m.searchQuery != "" {
		return m.searchRes
	}
	return m.entries
}
