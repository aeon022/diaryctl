package tui

import (
	"fmt"
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
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Design system ─────────────────────────────────────────────────────────────

var (
	colorGreen = lipgloss.AdaptiveColor{Light: "28", Dark: "42"}
	colorAmber = lipgloss.AdaptiveColor{Light: "214", Dark: "220"}
	colorMuted = lipgloss.AdaptiveColor{Light: "243", Dark: "246"}
	colorRed   = lipgloss.AdaptiveColor{Light: "160", Dark: "203"}
	selectedBg = lipgloss.AdaptiveColor{Light: "189", Dark: "17"}
	selectedFg = lipgloss.AdaptiveColor{Light: "16", Dark: "255"}
	colorBlue  = lipgloss.AdaptiveColor{Light: "25", Dark: "33"}
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).Foreground(colorBlue).Padding(0, 1)

	selectedStyle = lipgloss.NewStyle().
			Background(selectedBg).Foreground(selectedFg).Padding(0, 1)

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
	entries []models.Entry
	cursor  int

	// search
	searching   bool
	searchQuery string
	searchRes   []models.Entry

	// delete confirm
	confirmDelete bool
	deleteDate    time.Time

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
	repos      []models.Repo
	repoCursor int

	// today summary (loaded async after repos)
	todayCommits  int
	todayTasks    int
	todayEvents   int
	todayDuration time.Duration
	todayLoaded   bool

	// animation tick counter
	tickCount int
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

func New(s *store.Store) *Model {
	return &Model{
		store:    s,
		ta:       newTextarea(),
		wordGoal: 250,
	}
}

// ── Init ──────────────────────────────────────────────────────────────────────

func (m *Model) Init() tea.Cmd {
	return tea.Batch(cmdLoadEntries(m.store), cmdLoadRepos(m.store), cmdAnimTick())
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

		body := diary.BuildEntryBody(ds, tasks, events, times)
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
			m.flash("Deleted")
			if m.cursor > 0 {
				m.cursor--
			}
			m.confirmDelete = false
			return cmdLoadEntries(m.store)
		}
		m.confirmDelete = false
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
	case "n":
		m.flash("Generating today's entry…")
		return cmdGenerateToday(m.store)
	case "d":
		if len(entries) > 0 {
			m.confirmDelete = true
			m.deleteDate = entries[m.cursor].Date
		}
	case "r":
		m.view = repoView
		m.repoCursor = 0
	case "/":
		m.searching = true
		m.searchQuery = ""
	case "q":
		return tea.Quit
	}
	return nil
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
	case "d":
		if m.detail != nil {
			m.confirmDelete = true
			m.deleteDate = m.detail.Date
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
			_ = m.store.DeleteRepo(m.repos[m.repoCursor].Path)
			return cmdLoadRepos(m.store)
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
		if strings.Contains(strings.ToLower(e.Body), q) ||
			strings.Contains(e.Date.Format("2006-01-02"), q) {
			res = append(res, e)
		}
	}
	m.searchRes = res
	m.cursor = 0
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
	default:
		return m.viewList()
	}
}

func (m *Model) viewList() string {
	w, h := m.width, m.height
	if w < 40 {
		w = 80
	}
	if h < 20 {
		h = 24
	}

	heatW := 32
	listW := w - heatW - 6
	if listW < 20 {
		listW = 20
	}

	left := panelStyle.Width(heatW).Height(h - 6).Render(m.renderHeatmap())
	right := panelStyle.Width(listW).Height(h - 6).Render(m.renderEntryList(listW, h-6))
	top := lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)

	helpText := "j/k navigate  enter open  n new  e edit  d delete  r repos  / search  q quit"
	if m.confirmDelete {
		helpText = redStyle.Render(fmt.Sprintf(
			"Delete %s? y = confirm, any other key = cancel",
			m.deleteDate.Format("2006-01-02"),
		))
	}

	msg := ""
	if m.message != "" && time.Since(m.msgAt) < 5*time.Second {
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
		titleStyle.Render("diaryctl"),
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

	if m.searching {
		lines = append(lines, amberStyle.Render("/"+m.searchQuery+"_"))
	} else {
		lines = append(lines, titleStyle.Render("Entries"))
	}

	entries := m.visibleEntries()
	if len(entries) == 0 {
		lines = append(lines, "", mutedStyle.Render("No entries yet."),
			mutedStyle.Render("Press n to generate today's entry."))
		return strings.Join(lines, "\n")
	}

	maxVis := height - 3
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

		if i == m.cursor {
			// selectedStyle.Width(rowW) + Padding(0,1) = rowW+2 = width. No overflow.
			rowText := fmt.Sprintf("%-12s  %-*s%s", dateStr, maxP, preview, tagPlain)
			lines = append(lines, selectedStyle.Width(rowW).Render(rowText))
		} else {
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

	header := titleStyle.Render(m.detail.Date.Format("2006-01-02"))
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
		helpStyle.Render("j/k scroll  e edit  d delete  esc back"),
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
		titleStyle.Render("diaryctl — editor"),
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
	lines = append(lines, titleStyle.Render("Registered Repos"), "")
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
	lines = append(lines, "", helpStyle.Render("j/k navigate  d delete  esc back"))
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
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}
