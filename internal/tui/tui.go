package tui

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
	"unicode"

	"github.com/aeon022/diaryctl/internal/ai"
	"github.com/aeon022/diaryctl/internal/diary"
	"github.com/aeon022/diaryctl/internal/git"
	"github.com/aeon022/diaryctl/internal/models"
	"github.com/aeon022/diaryctl/internal/notectl"
	"github.com/aeon022/diaryctl/internal/store"
	"github.com/aeon022/diaryctl/internal/suite"
	"github.com/aeon022/missionctl-core/keymap"
	"github.com/aeon022/missionctl-core/overlay"
	"github.com/aeon022/missionctl-core/palette"
	"github.com/aeon022/missionctl-core/theme"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
)

// ── Design system ─────────────────────────────────────────────────────────────

var (
	// Shared across the suite via missionctl-core/theme.
	colorGreen = theme.Green
	colorAmber = theme.Amber
	colorMuted = theme.Muted
	colorRed   = theme.Red
	selectedBg = theme.SelectedBg
	selectedFg = theme.SelectedFg
	colorBlue  = theme.Blue
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).Foreground(colorBlue).Padding(0, 1)

	selectedStyle = lipgloss.NewStyle().
			Background(selectedBg).Foreground(selectedFg).Padding(0, 1)
	// hoverStyle matches selectedStyle's Padding(0, 1) so a hovered row
	// renders at the same total width as a selected one (theme.Hover
	// itself carries no padding, since that's context-specific).
	hoverStyle = theme.Hover.Padding(0, 1)

	normalStyle = lipgloss.NewStyle().Padding(0, 1)
	mutedStyle  = lipgloss.NewStyle().Foreground(colorMuted)
	amberStyle  = lipgloss.NewStyle().Foreground(colorAmber).Bold(true)
	greenStyle  = lipgloss.NewStyle().Foreground(colorGreen)
	redStyle    = lipgloss.NewStyle().Foreground(colorRed).Bold(true)
	helpStyle   = lipgloss.NewStyle().Foreground(colorMuted).Italic(true)
	statusStyle = lipgloss.NewStyle().Foreground(colorMuted).Padding(0, 1)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorMuted).Padding(0, 1)

	editorBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorGreen).Padding(0, 1)
)

// ── Views ─────────────────────────────────────────────────────────────────────

type viewType int

const (
	listView   viewType = iota
	detailView viewType = iota
	editorView viewType = iota
	repoView   viewType = iota
	helpView   viewType = iota
)

// ── Messages ──────────────────────────────────────────────────────────────────

type (
	entriesLoadedMsg struct{ entries []models.Entry }
	reposLoadedMsg   struct{ repos []models.Repo }
	entryGenMsg      struct {
		entry *models.Entry
		err   error
	}
	autoSaveTickMsg struct{}
	errMsg          struct{ err error }

	aiChunkMsg struct{ chunk string }
	aiDoneMsg  struct{ full string }
	aiErrMsg   struct{ err error }

	todaySummaryMsg struct {
		commits  int
		tasks    int
		events   int
		duration time.Duration
	}
	animTickMsg struct{}
)

// ── Model ─────────────────────────────────────────────────────────────────────

type Model struct {
	store  *store.Store
	view   viewType
	width  int
	height int
	streak int
	err    error

	// flash message
	message string
	msgAt   time.Time

	// list
	entries      []models.Entry
	cursor       int
	hoverRow     int // visibleEntries() index under the mouse cursor, -1 when none
	lastClickRow int // visibleEntries() index of the previous left-click, -1 when none — double-click opens the entry detail, same window/pattern taskctl uses
	lastClickAt  time.Time

	// search
	searching   bool
	searchQuery string
	searchRes   []models.Entry

	// ":" command palette
	inPalette     bool
	paletteQuery  string
	paletteCursor int

	// delete confirm
	confirmDelete bool
	deleteDate    time.Time
	deleteTarget  *models.Entry // full entry captured at "d" time, for undo

	// undo: "u" within undoWindow of a delete restores the deleted entry —
	// same pattern and window taskctl uses for its own delete-undo.
	lastDeleted *models.Entry

	// detail
	detail     *models.Entry
	detailScrl int

	// editor
	editorEntry     *models.Entry
	ta              textarea.Model
	editorDirty     bool
	lastSaved       time.Time
	savedFlash      bool
	centeredMode    bool
	wordGoal        int
	vimNormal       bool
	aiBeforeContent string

	// AI streaming
	aiGenerating bool
	aiTokens     int
	aiChan       chan ai.StreamResult

	// repos
	repos             []models.Repo
	repoCursor        int
	confirmDeleteRepo bool         // "d" was pressed once, waiting on y/Y to confirm — this view had no confirm step before, unlike every other destructive action in the app
	lastDeletedRepo   *models.Repo // undo: "u" within undoWindow restores it, same pattern as entry delete-undo

	// today summary (loaded async after repos)
	todayCommits  int
	todayTasks    int
	todayEvents   int
	todayDuration time.Duration
	todayLoaded   bool

	// animation tick counter
	tickCount int

	// "?" transient help popup
	helpVP   viewport.Model
	helpPopW int
	helpPopH int
}

func newTextarea() textarea.Model {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.Placeholder = ""
	ta.FocusedStyle.Base = lipgloss.NewStyle()
	ta.BlurredStyle.Base = lipgloss.NewStyle()
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Prompt = lipgloss.NewStyle()
	ta.BlurredStyle.Prompt = lipgloss.NewStyle()
	return ta
}

// ── command palette (":") ────────────────────────────────────────────────────
//
// Types out full words instead of memorizing single-key shortcuts. Reuses
// the exact same key handling every shortcut already goes through
// (handleList) by replaying the mapped keypress, so behavior is guaranteed
// identical to typing the key directly. Matching logic lives in
// missionctl-core/palette (shared across the suite); this list is
// diaryctl-specific.
var paletteCommands = []palette.Command{
	{Name: "new", Desc: "Generate today's entry", Key: "n"},
	{Name: "edit", Desc: "Edit entry", Key: "e"},
	{Name: "delete", Desc: "Delete entry (asks to confirm)", Key: "d"},
	{Name: "open", Desc: "Open entry", Key: "enter"},
	{Name: "copy", Desc: "Copy title to clipboard", Key: "y"},
	{Name: "undo", Desc: "Undo last delete", Key: "u"},
	{Name: "goto", Desc: "Open corresponding note in notectl", Key: "g"},
	{Name: "repos", Desc: "Browse tracked git repos", Key: "r"},
	{Name: "search", Desc: "Search entries", Key: "/"},
	{Name: "help", Desc: "Show help", Key: "?"},
	{Name: "quit", Desc: "Quit diaryctl", Key: "q"},
}

func New(s *store.Store) *Model {
	return &Model{
		store:        s,
		ta:           newTextarea(),
		wordGoal:     250,
		hoverRow:     -1,
		lastClickRow: -1,
	}
}

// ── Init ──────────────────────────────────────────────────────────────────────

func (m *Model) Init() tea.Cmd {
	return tea.Batch(cmdLoadEntries(m.store), cmdLoadRepos(m.store), cmdAnimTick())
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

// copyToClipboardCmd shells out to pbcopy — same approach taskctl/mailctl/
// notectl/calctl/habctl/timectl use for their own "y" copy shortcuts, no
// clipboard library needed.
func copyToClipboardCmd(text string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(text)
		_ = cmd.Run()
		return nil
	}
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

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.view == editorView {
			m.resizeEditor()
		}
		return m, nil

	case entriesLoadedMsg:
		m.entries = msg.entries
		m.streak, _ = m.store.GetStreak()
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
			before := countWords(m.aiBeforeContent)
			after := countWords(msg.full)
			m.ta.SetValue(msg.full)
			m.editorDirty = true
			m.flash(fmt.Sprintf("Claude wrote %d words (+%d) — review and ctrl+s to save", after, after-before))
		} else {
			m.flash("Claude finished — review and ctrl+s to save")
		}
		return m, nil

	case aiErrMsg:
		m.aiGenerating = false
		m.flash("AI error: " + msg.err.Error())
		return m, nil

	case errMsg:
		m.err = msg.err
		return m, nil

	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			if m.view == listView && m.cursor > 0 {
				m.cursor--
			}
		case tea.MouseButtonWheelDown:
			if m.view == listView && m.cursor < len(m.visibleEntries())-1 {
				m.cursor++
			}
		case tea.MouseButtonLeft:
			if msg.Action != tea.MouseActionPress || m.view != listView {
				return m, nil
			}
			if i := m.rowHitTest(msg.X, msg.Y); i >= 0 {
				now := time.Now()
				if i == m.lastClickRow && now.Sub(m.lastClickAt) < doubleClickWindow {
					m.cursor = i
					m.lastClickRow = -1 // consumed, so a third click starts fresh
					e := m.visibleEntries()[i]
					m.detail = &e
					m.detailScrl = 0
					m.view = detailView
					return m, nil
				}
				m.cursor = i
				m.lastClickRow = i
				m.lastClickAt = now
			}
		case tea.MouseButtonNone:
			if msg.Action == tea.MouseActionMotion && m.view == listView {
				m.hoverRow = m.rowHitTest(msg.X, msg.Y)
			}
		}
		return m, nil

	case tea.KeyMsg:
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

func (m *Model) handleList(msg tea.KeyMsg) tea.Cmd {
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
			replay := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(chosen.Key)}
			if chosen.Key == "enter" {
				replay = tea.KeyMsg{Type: tea.KeyEnter}
			}
			return m.handleList(replay)
		case "backspace":
			if len(m.paletteQuery) > 0 {
				m.paletteQuery = m.paletteQuery[:len(m.paletteQuery)-1]
			}
		default:
			if len(msg.String()) == 1 {
				m.paletteQuery += msg.String()
			}
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
			if len(m.searchQuery) > 0 {
				m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
				m.filterEntries()
			}
		default:
			if len(msg.String()) == 1 {
				m.searchQuery += msg.String()
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
		h := m.height
		if h < 20 {
			h = 24
		}
		maxVis := (h - 6) - 3
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
			m.detail = &e
			m.detailScrl = 0
			m.view = detailView
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

func (m *Model) handleHelp(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "q", "esc", "?":
		m.view = listView
		return nil
	}
	var cmd tea.Cmd
	m.helpVP, cmd = m.helpVP.Update(msg)
	return cmd
}

func (m *Model) handleDetail(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "q":
		m.view = listView
		m.detail = nil
	case "j", "down":
		m.detailScrl++
	case "k", "up":
		if m.detailScrl > 0 {
			m.detailScrl--
		}
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

func (m *Model) handleEditor(msg tea.KeyMsg) tea.Cmd {
	// Vim normal mode — intercept keys before passing to textarea.
	if m.vimNormal {
		switch msg.String() {
		case "i", "a", "A", "o", "O":
			m.vimNormal = false
		case "h":
			var cmd tea.Cmd
			m.ta, cmd = m.ta.Update(tea.KeyMsg{Type: tea.KeyLeft})
			return cmd
		case "l":
			var cmd tea.Cmd
			m.ta, cmd = m.ta.Update(tea.KeyMsg{Type: tea.KeyRight})
			return cmd
		case "j":
			var cmd tea.Cmd
			m.ta, cmd = m.ta.Update(tea.KeyMsg{Type: tea.KeyDown})
			return cmd
		case "k":
			var cmd tea.Cmd
			m.ta, cmd = m.ta.Update(tea.KeyMsg{Type: tea.KeyUp})
			return cmd
		case "0":
			var cmd tea.Cmd
			m.ta, cmd = m.ta.Update(tea.KeyMsg{Type: tea.KeyHome})
			return cmd
		case "$":
			var cmd tea.Cmd
			m.ta, cmd = m.ta.Update(tea.KeyMsg{Type: tea.KeyEnd})
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

	case "a":
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

func (m *Model) handleRepo(msg tea.KeyMsg) tea.Cmd {
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

// ── View ──────────────────────────────────────────────────────────────────────

func (m *Model) View() string {
	if m.err != nil {
		return redStyle.Render("Error: "+m.err.Error()) + "\n\nPress q to quit."
	}
	switch m.view {
	case detailView:
		return m.viewDetail()
	case editorView:
		return m.viewEditor()
	case repoView:
		return m.viewRepos()
	case helpView:
		// "?" is only reachable from the main list, so the list is always
		// the correct background to keep visible behind the popup. No
		// enclosing border on the list view, so inset 0 is safe.
		return overlay.Center(m.viewList(), m.renderHelpPopup(), m.width, m.height, 0)
	default:
		return m.viewList()
	}
}

// renderHeader is the one header shared by every view: app name + current
// section, so it stays a constant anchor no matter which screen is active.
func (m *Model) renderHeader(section string) string {
	// titleStyle already has Padding(0, 1), which supplies the leading
	// and trailing space — don't double it up here.
	return titleStyle.Render("diaryctl") + mutedStyle.Render("· "+section)
}

func (m *Model) helpContent() string {
	return keymap.New("diaryctl", "developer diary from the terminal").
		Section("Navigation").
		Row("j / k", "move down / up").
		Row("enter", "open entry").
		Row("/", "search entries (esc clears)").
		Row(":", "command palette — type an action by name").
		Row("r", "browse tracked git repos").
		Section("Entries").
		Row("n", "generate today's entry").
		Row("e", "edit entry").
		Row("d", "delete entry (asks to confirm)").
		Row("g", "open corresponding note in notectl").
		Section("Editor").
		Row("ctrl+s", "save").
		Row("esc", "save (if dirty) and close").
		Row("a", "ask Claude to continue the entry").
		Row("ctrl+f", "toggle centered writing mode").
		Row("ctrl+v", "vim normal mode (hjkl, i/a/o to insert)").
		Row("tab/[/]", "jump to next AI marker / section").
		Section("Other").
		Row("?", "toggle this help").
		Row("q", "quit").
		String()
}

// openHelp sizes and populates the transient help popup (see
// renderHelpPopup/overlay.Center) from the ACTUAL rendered background
// height, not the terminal size.
func (m *Model) openHelp() {
	bgLines := strings.Split(m.viewList(), "\n")

	safeH := max(6, len(bgLines))
	popH := min(safeH, 22)
	popW := min(70, m.width)
	if popW < 40 {
		popW = 40
	}

	vp := viewport.New(popW-6, popH-5) // border 1+1, padding(1,2) → 2 rows/4 cols; -1 row for footer
	vp.SetContent(m.helpContent())

	m.helpVP = vp
	m.helpPopW = popW
	m.helpPopH = popH
	m.view = helpView
}

// renderHelpPopup renders the help viewport in a bordered box, meant to be
// composited over the list view via overlay.Center rather than replacing
// the whole screen — the list stays visible around it.
func (m *Model) renderHelpPopup() string {
	footer := "esc / ?  close"
	if m.helpVP.TotalLineCount() > m.helpVP.Height {
		footer = fmt.Sprintf("j/k scroll (%d%%)  ·  %s", int(m.helpVP.ScrollPercent()*100), footer)
	}
	body := m.helpVP.View() + "\n" + mutedStyle.Render(footer)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBlue).
		Padding(1, 2).
		Width(m.helpPopW).
		Render(body)
}

// heatmapPanelW is the heatmap (left) panel's fixed outer width — shared
// with rowHitTest so the entry list's screen X-offset can't drift from
// what viewList actually renders.
const heatmapPanelW = 32

// doubleClickWindow opens the entry detail on a second click within this
// window, same pattern and duration taskctl uses for its own double-click.
const doubleClickWindow = 400 * time.Millisecond

// undoWindow is how long after a delete "u" still restores it — same
// duration taskctl uses for its own delete-undo.
const undoWindow = 5 * time.Second

func (m *Model) viewList() string {
	w, h := m.width, m.height
	if w < 40 {
		w = 80
	}
	if h < 20 {
		h = 24
	}

	heatW := heatmapPanelW
	listW := w - heatW - 6
	if listW < 20 {
		listW = 20
	}

	left := panelStyle.Width(heatW).Height(h - 6).Render(m.renderHeatmap())
	right := panelStyle.Width(listW).Height(h - 6).Render(m.renderEntryList(listW, h-6))
	top := lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)

	helpText := "j/k:navigate  enter:open  n:new  e:edit  d:delete  u:undo  y:copy  g:open note  r:repos  /:search  ?:help  q:quit"
	if m.confirmDelete {
		helpText = redStyle.Render(fmt.Sprintf(
			"Delete %s? y = confirm, any other key = cancel",
			m.deleteDate.Format("2006-01-02"),
		))
	}

	msg := ""
	flashDur := 3 * time.Second
	if m.lastDeleted != nil {
		flashDur = undoWindow
	}
	if m.message != "" && time.Since(m.msgAt) < flashDur {
		msg = "  " + greenStyle.Render(m.message)
	}

	// Streak with pulsing animation for streaks > 7 days.
	streakStr := amberStyle.Render(fmt.Sprintf("streak %dd", m.streak))
	if m.streak > 7 {
		var flame string
		if m.tickCount%2 == 0 {
			flame = amberStyle.Render("🔥 ")
		} else {
			flame = redStyle.Render("🔥 ")
		}
		streakStr = flame + streakStr
	}

	statusLine := statusStyle.Render(
		streakStr +
			"  " + mutedStyle.Render(fmt.Sprintf("%d entries", len(m.entries))) +
			msg,
	)

	return lipgloss.JoinVertical(lipgloss.Left,
		m.renderHeader("Journal"),
		top,
		statusLine,
		helpStyle.Render(helpText),
	)
}

func (m *Model) renderHeatmap() string {
	today := time.Now()
	commitMap := make(map[string]int)
	for _, e := range m.entries {
		k := e.Date.Format("2006-01-02")
		if e.Body != "" {
			commitMap[k] = len(strings.Split(e.Body, "\n- `"))
		}
	}

	// Current entry's date — highlight in heatmap.
	selectedKey := ""
	entries := m.visibleEntries()
	if m.cursor >= 0 && m.cursor < len(entries) {
		selectedKey = entries[m.cursor].Date.Format("2006-01-02")
	}

	days := make([]time.Time, 30)
	for i := range days {
		days[29-i] = today.AddDate(0, 0, -i)
	}

	cells := make([]string, int(days[0].Weekday()))
	for i := range cells {
		cells[i] = "  "
	}
	for _, d := range days {
		key := d.Format("2006-01-02")
		if key == selectedKey {
			cells = append(cells, amberStyle.Bold(true).Render("█"))
		} else {
			cells = append(cells, heatCell(commitMap[key]))
		}
	}
	for len(cells)%7 != 0 {
		cells = append(cells, "  ")
	}

	var lines []string
	lines = append(lines, mutedStyle.Render("last 30 days"))
	lines = append(lines, "")
	lines = append(lines, mutedStyle.Render("S M T W T F S"))
	for i := 0; i < len(cells); i += 7 {
		lines = append(lines, strings.Join(cells[i:i+7], " "))
	}

	// Today summary loaded asynchronously after repos.
	if m.todayLoaded {
		lines = append(lines, "")
		lines = append(lines, mutedStyle.Render("today"))
		var parts []string
		if m.todayCommits > 0 {
			parts = append(parts, greenStyle.Render(fmt.Sprintf("%d commit%s", m.todayCommits, plural(m.todayCommits))))
		}
		if m.todayTasks > 0 {
			parts = append(parts, fmt.Sprintf("%d task%s", m.todayTasks, plural(m.todayTasks)))
		}
		if m.todayEvents > 0 {
			parts = append(parts, fmt.Sprintf("%d event%s", m.todayEvents, plural(m.todayEvents)))
		}
		if m.todayDuration > 0 {
			parts = append(parts, formatDuration(m.todayDuration))
		}
		if len(parts) > 0 {
			lines = append(lines, strings.Join(parts, " · "))
		} else {
			lines = append(lines, mutedStyle.Render("nothing yet"))
		}
	}

	return strings.Join(lines, "\n")
}

func heatCell(count int) string {
	b := "█"
	switch {
	case count == 0:
		return mutedStyle.Render(b)
	case count <= 2:
		return lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "71", Dark: "22"}).Render(b)
	case count <= 5:
		return greenStyle.Render(b)
	default:
		return greenStyle.Bold(true).Render(b)
	}
}

func (m *Model) renderEntryList(width, height int) string {
	var lines []string

	paletteLines := 0
	if m.inPalette {
		lines = append(lines, amberStyle.Render(": "+m.paletteQuery+"_"))
		matches := palette.Match(paletteCommands, m.paletteQuery)
		if len(matches) > 6 {
			matches = matches[:6]
		}
		if len(matches) == 0 {
			lines = append(lines, mutedStyle.Render("  no matching command"))
		}
		for i, c := range matches {
			row := fmt.Sprintf("%-9s %s", c.Name, c.Desc)
			if i == m.paletteCursor {
				lines = append(lines, greenStyle.Render("▶ "+row))
			} else {
				lines = append(lines, mutedStyle.Render("  "+row))
			}
		}
		paletteLines = len(lines) - 1 // extra lines beyond the usual single title/search line "-3" already budgets for
	} else if m.searching {
		lines = append(lines, amberStyle.Render("/"+m.searchQuery+"_"))
	} else {
		lines = append(lines, titleStyle.Render("Entries"))
	}

	entries := m.visibleEntries()
	if len(entries) == 0 {
		lines = append(lines, "", mutedStyle.Render("No entries yet — press n to generate today's entry."))
		return strings.Join(lines, "\n")
	}

	maxVis := height - 3 - paletteLines
	start := 0
	if m.cursor >= maxVis {
		start = m.cursor - maxVis + 1
	}

	// rowW = content width available before adding the 1-space side padding.
	// Layout per row: " " + "%-12s  " (14) + preview (maxP) + tag (0|5) + " " = width
	// So: maxP = rowW - 14 - tagLen, and rowW = width - 2.
	rowW := width - 2
	if rowW < 10 {
		rowW = 10
	}

	for i, e := range entries {
		if i < start {
			continue
		}
		if i-start >= maxVis {
			break
		}
		dateStr := e.Date.Format("2006-01-02")

		tagPlain := ""
		tagStyled := ""
		if e.Generated {
			tagPlain = " [AI]"
			tagStyled = " " + greenStyle.Render("[AI]")
		}

		maxP := rowW - 14 - len(tagPlain)
		if maxP < 0 {
			maxP = 0
		}
		preview := firstLine(e.Body)
		if len(preview) > maxP {
			preview = preview[:maxP] + "…"
		}

		switch {
		case i == m.cursor, i == m.hoverRow:
			// selectedStyle/hoverStyle.Width(rowW) + Padding(0,1) = rowW+2 = width. No overflow.
			rowText := fmt.Sprintf("%-12s  %-*s%s", dateStr, maxP, preview, tagPlain)
			if i == m.cursor {
				lines = append(lines, selectedStyle.Width(rowW).Render(rowText))
			} else {
				lines = append(lines, hoverStyle.Width(rowW).Render(rowText))
			}
		default:
			// Build styled row without nesting ANSI inside fmt.Sprintf — avoids
			// lipgloss width miscalculation on content with embedded escape codes.
			var previewStyled string
			if m.searchQuery != "" {
				previewStyled = highlightMatch(preview, m.searchQuery)
			} else {
				previewStyled = mutedStyle.Render(preview)
			}
			// Manual Padding(0,1): one space on each side.
			row := " " + fmt.Sprintf("%-12s  ", dateStr) + previewStyled + tagStyled + " "
			lines = append(lines, row)
		}
	}
	return strings.Join(lines, "\n")
}

// rowHitTest returns the visibleEntries() index at screen position (x, y),
// or -1 if the click missed. Mirrors viewList's w/h fallback and panel
// sizing and renderEntryList's exact layout (1 title/search line, then
// entries, scrolled via the same start := cursor-maxVis+1 window) so a
// click lands on the entry it visually appears to be over. x must land
// inside the entry list (right) panel, past the heatmap panel + gap.
func (m *Model) rowHitTest(x, y int) int {
	if x < heatmapPanelW+2 {
		return -1
	}
	h := m.height
	if h < 20 {
		h = 24
	}
	panelHeight := h - 6

	entries := m.visibleEntries()
	if len(entries) == 0 {
		return -1
	}
	maxVis := panelHeight - 3
	start := 0
	if m.cursor >= maxVis {
		start = m.cursor - maxVis + 1
	}

	idx := y - 3
	if idx < 0 {
		return -1
	}
	i := start + idx
	if i >= len(entries) || idx >= maxVis {
		return -1
	}
	return i
}

func (m *Model) viewDetail() string {
	if m.detail == nil {
		return "No entry selected."
	}
	w, h := m.width, m.height
	if w < 40 {
		w = 80
	}
	if h < 20 {
		h = 24
	}

	header := m.renderHeader("Entry") + "  " + amberStyle.Render(m.detail.Date.Format("2006-01-02"))
	if m.detail.Generated {
		header += " " + greenStyle.Render("[AI]")
	}

	bodyLines := strings.Split(m.detail.Body, "\n")
	end := m.detailScrl + h - 6
	if end > len(bodyLines) {
		end = len(bodyLines)
	}
	start := m.detailScrl
	if start > len(bodyLines) {
		start = len(bodyLines)
	}
	rendered := renderMarkdown(strings.Join(bodyLines[start:end], "\n"))
	body := panelStyle.Width(w - 4).Render(rendered)

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		body,
		helpStyle.Render("j/k scroll  e edit  d delete  g open note  esc back"),
	)
}

func (m *Model) viewEditor() string {
	w := m.width
	if w < 40 {
		w = 80
	}

	content := m.ta.Value()
	wc := countWords(content)
	sec := currentSection(content, m.ta.Line())
	aiBlocks := strings.Count(content, "<!-- AI:")

	saveStr := ""
	if m.savedFlash && time.Since(m.lastSaved) < 3*time.Second {
		saveStr = "  " + greenStyle.Render("✓ saved")
	} else if m.editorDirty {
		saveStr = "  " + mutedStyle.Render("●")
	}

	date := ""
	if m.editorEntry != nil {
		date = amberStyle.Render(m.editorEntry.Date.Format("2006-01-02")) + "  "
	}
	secStr := ""
	if sec != "" {
		secStr = "  " + mutedStyle.Render("§"+sec)
	}

	// Word count progress bar toward wordGoal.
	wcStr := wordProgress(wc, m.wordGoal, 8)

	// Vim mode indicator.
	modeStr := ""
	if m.vimNormal {
		modeStr = "  " + lipgloss.NewStyle().Foreground(colorBlue).Bold(true).Render("[N]")
	}

	statusLeft := statusStyle.Render(date + wcStr + secStr + saveStr + modeStr)

	aKey := "a ask claude"
	if m.aiGenerating {
		aKey = "a writing…"
	}
	vimHint := "ctrl+v vim"
	if m.vimNormal {
		vimHint = "i insert  hjkl move"
	}
	keysRight := mutedStyle.Render(fmt.Sprintf("ctrl+s save  %s  [ ] jump  ctrl+f focus  %s  esc done", aKey, vimHint))
	gap := w - lipgloss.Width(statusLeft) - lipgloss.Width(keysRight)
	if gap < 1 {
		gap = 1
	}
	statusBar := statusLeft + strings.Repeat(" ", gap) + keysRight

	aiHint := ""
	if m.aiGenerating {
		dots := [4]string{"⠋", "⠙", "⠹", "⠸"}
		spin := dots[time.Now().UnixMilli()/120%4]
		aiHint = "  " + amberStyle.Render(fmt.Sprintf("%s Claude writing… %d words", spin, m.aiTokens))
	} else if aiBlocks > 0 {
		aiHint = "  " + mutedStyle.Render(fmt.Sprintf("%d AI prompt%s · a to fill · tab to jump", aiBlocks, plural(aiBlocks)))
	}
	header := lipgloss.JoinHorizontal(lipgloss.Center,
		m.renderHeader("Editor"),
		aiHint,
	)

	var editorBlock string
	if m.centeredMode {
		tw := m.ta.Width()
		pad := (w - tw - 6) / 2
		if pad < 0 {
			pad = 0
		}
		margin := strings.Repeat(" ", pad)
		editorBlock = margin + editorBorder.Width(tw+2).Render(m.ta.View())
	} else {
		editorBlock = editorBorder.Width(w - 4).Render(m.ta.View())
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, editorBlock, statusBar)
}

func (m *Model) viewRepos() string {
	var lines []string
	lines = append(lines, m.renderHeader("Repos"), "")
	if len(m.repos) == 0 {
		lines = append(lines,
			mutedStyle.Render("No repos registered."),
			mutedStyle.Render("Run: diaryctl init [path]"),
		)
	} else {
		for i, r := range m.repos {
			line := fmt.Sprintf("%-20s %s", r.Name, r.Path)
			if i == m.repoCursor {
				lines = append(lines, selectedStyle.Render(line))
			} else {
				lines = append(lines, normalStyle.Render(line))
			}
		}
	}
	var footer string
	switch {
	case m.confirmDeleteRepo && len(m.repos) > 0:
		footer = redStyle.Render(fmt.Sprintf(
			"Delete %s? y = confirm, any other key = cancel", m.repos[m.repoCursor].Name,
		))
	case m.message != "" && time.Since(m.msgAt) < undoWindow:
		footer = greenStyle.Render(m.message)
	default:
		footer = helpStyle.Render("j/k:navigate  d:delete  u:undo  esc:back")
	}
	lines = append(lines, "", footer)
	return strings.Join(lines, "\n")
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// renderMarkdown colorizes markdown for the detail view.
func renderMarkdown(s string) string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		switch {
		case strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "# "):
			lines = append(lines, greenStyle.Bold(true).Render(line))
		case strings.HasPrefix(line, "### "):
			lines = append(lines, lipgloss.NewStyle().Foreground(colorGreen).Render(line))
		case strings.HasPrefix(line, "<!-- AI:"):
			lines = append(lines, amberStyle.Render(line))
		case strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* "):
			lines = append(lines, mutedStyle.Render("·")+" "+line[2:])
		default:
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

// wordProgress renders a progress bar + word count toward goal.
func wordProgress(current, goal, barWidth int) string {
	if goal <= 0 {
		return mutedStyle.Render(fmt.Sprintf("%dw", current))
	}
	pct := float64(current) / float64(goal)
	if pct > 1 {
		pct = 1
	}
	filled := int(pct * float64(barWidth))
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	col := colorMuted
	if current >= goal {
		col = colorGreen
	}
	barStr := lipgloss.NewStyle().Foreground(col).Render("[" + bar + "]")
	return barStr + " " + mutedStyle.Render(fmt.Sprintf("%d/%dw", current, goal))
}

// highlightMatch returns s with the first occurrence of q rendered in amber,
// surrounding text rendered muted.
func highlightMatch(s, q string) string {
	if q == "" {
		return mutedStyle.Render(s)
	}
	lower := strings.ToLower(s)
	lq := strings.ToLower(q)
	idx := strings.Index(lower, lq)
	if idx < 0 {
		return mutedStyle.Render(s)
	}
	before := mutedStyle.Render(s[:idx])
	match := amberStyle.Render(s[idx : idx+len(q)])
	after := mutedStyle.Render(s[idx+len(q):])
	return before + match + after
}

// formatDuration returns a compact human-readable duration like "4h 20m".
func formatDuration(d time.Duration) string {
	h := int(d.Hours())
	mins := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "<!--") {
			line = strings.TrimPrefix(line, "- ")
			line = strings.TrimPrefix(line, "* ")
			return line
		}
	}
	return "(empty)"
}

func countWords(s string) int {
	n, inWord := 0, false
	for _, r := range s {
		if unicode.IsSpace(r) {
			inWord = false
		} else if !inWord {
			inWord = true
			n++
		}
	}
	return n
}

func currentSection(content string, cursorLine int) string {
	sec := ""
	for i, line := range strings.Split(content, "\n") {
		if i > cursorLine {
			break
		}
		if strings.HasPrefix(line, "## ") {
			sec = strings.TrimPrefix(line, "## ")
		}
	}
	return sec
}

func lineToOffset(content string, targetLine int) int {
	line := 0
	for i, ch := range content {
		if line == targetLine {
			return i
		}
		if ch == '\n' {
			line++
		}
	}
	return len(content)
}

func offsetToLine(content string, offset int) int {
	line := 0
	for i, ch := range content {
		if i >= offset {
			break
		}
		if ch == '\n' {
			line++
		}
	}
	return line
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// ── Run ───────────────────────────────────────────────────────────────────────

func Run(s *store.Store) error {
	m := New(s)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseAllMotion())
	_, err := p.Run()
	return err
}
