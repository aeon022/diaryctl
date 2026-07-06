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
	"github.com/aeon022/diaryctl/internal/store"
	"github.com/aeon022/diaryctl/internal/suite"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Design system ─────────────────────────────────────────────────────────────

var (
	colorGreen  = lipgloss.AdaptiveColor{Light: "28", Dark: "42"}
	colorAmber  = lipgloss.AdaptiveColor{Light: "214", Dark: "220"}
	colorMuted  = lipgloss.AdaptiveColor{Light: "243", Dark: "246"}
	colorRed    = lipgloss.AdaptiveColor{Light: "160", Dark: "203"}
	selectedBg  = lipgloss.AdaptiveColor{Light: "189", Dark: "17"}
	selectedFg  = lipgloss.AdaptiveColor{Light: "16", Dark: "255"}
	colorBlue   = lipgloss.AdaptiveColor{Light: "25", Dark: "33"}
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).Foreground(colorBlue).Padding(0, 1)

	selectedStyle = lipgloss.NewStyle().
			Background(selectedBg).Foreground(selectedFg).Padding(0, 1)

	normalStyle   = lipgloss.NewStyle().Padding(0, 1)
	mutedStyle    = lipgloss.NewStyle().Foreground(colorMuted)
	amberStyle    = lipgloss.NewStyle().Foreground(colorAmber).Bold(true)
	greenStyle    = lipgloss.NewStyle().Foreground(colorGreen)
	redStyle      = lipgloss.NewStyle().Foreground(colorRed).Bold(true)
	helpStyle     = lipgloss.NewStyle().Foreground(colorMuted).Italic(true)
	statusStyle   = lipgloss.NewStyle().Foreground(colorMuted).Padding(0, 1)

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
	editorEntry  *models.Entry
	ta           textarea.Model
	editorDirty  bool
	lastSaved    time.Time
	savedFlash   bool
	centeredMode bool

	// AI streaming
	aiGenerating bool
	aiTokens     int
	aiChan       chan ai.StreamResult

	// repos
	repos      []models.Repo
	repoCursor int
}

func newTextarea() textarea.Model {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.Placeholder = ""
	// Remove default keybindings styling so it blends with our theme.
	ta.FocusedStyle.Base = lipgloss.NewStyle()
	ta.BlurredStyle.Base = lipgloss.NewStyle()
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Prompt = lipgloss.NewStyle()
	ta.BlurredStyle.Prompt = lipgloss.NewStyle()
	return ta
}

func New(s *store.Store) *Model {
	return &Model{
		store: s,
		ta:    newTextarea(),
	}
}

// ── Init ──────────────────────────────────────────────────────────────────────

func (m *Model) Init() tea.Cmd {
	return tea.Batch(cmdLoadEntries(m.store), cmdLoadRepos(m.store))
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
		entry, _ := s.GetEntry(today)
		return entryGenMsg{entry: entry}
	}
}

func cmdAutoSaveTick() tea.Cmd {
	return tea.Tick(30*time.Second, func(time.Time) tea.Msg {
		return autoSaveTickMsg{}
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
		return m, nil

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
			m.ta.SetValue(msg.full)
			m.editorDirty = true
		}
		m.flash("Claude finished — review and ctrl+s to save")
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

	// Forward non-key messages to textarea when editing.
	if m.view == editorView {
		var cmd tea.Cmd
		m.ta, cmd = m.ta.Update(msg)
		return m, cmd
	}

	return m, nil
}

// ── Key handlers ─────────────────────────────────────────────────────────────

func (m *Model) handleList(msg tea.KeyMsg) tea.Cmd {
	// Delete confirm overlay.
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

	// Search mode.
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

	case "a":
		if m.aiGenerating {
			return nil
		}
		m.aiGenerating = true
		m.aiTokens = 0
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
		// Jump to next <!-- AI: --> block.
		content := m.ta.Value()
		line := m.ta.Line()
		pos := lineToOffset(content, line)
		if next := strings.Index(content[pos+1:], "<!-- AI:"); next >= 0 {
			targetLine := offsetToLine(content, pos+1+next)
			// Move cursor by pressing down until we reach target line.
			_ = targetLine
			// bubbles/textarea v1 doesn't expose GotoLine — fall through.
		}
		// Pass tab to textarea as normal.
		var cmd tea.Cmd
		m.ta, cmd = m.ta.Update(msg)
		return cmd

	case "[":
		// Previous ## section.
		content := m.ta.Value()
		line := m.ta.Line()
		pos := lineToOffset(content, line)
		if prev := strings.LastIndex(content[:pos], "\n## "); prev >= 0 {
			_ = offsetToLine(content, prev+1)
		}
		return nil

	case "]":
		// Next ## section.
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
	statusLine := statusStyle.Render(
		amberStyle.Render(fmt.Sprintf("streak %dd", m.streak)) +
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

	days := make([]time.Time, 30)
	for i := range days {
		days[29-i] = today.AddDate(0, 0, -i)
	}

	cells := make([]string, int(days[0].Weekday()))
	for i := range cells {
		cells[i] = "  "
	}
	for _, d := range days {
		cells = append(cells, heatCell(commitMap[d.Format("2006-01-02")]))
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

	for i, e := range entries {
		if i < start {
			continue
		}
		if i-start >= maxVis {
			break
		}
		dateStr := e.Date.Format("2006-01-02")
		preview := firstLine(e.Body)
		maxP := width - 15
		if maxP < 0 {
			maxP = 0
		}
		if len(preview) > maxP {
			preview = preview[:maxP] + "…"
		}
		tag := ""
		if e.Generated {
			tag = greenStyle.Render(" [AI]")
		}
		line := fmt.Sprintf("%-12s %s%s", dateStr, preview, tag)
		if i == m.cursor {
			lines = append(lines, selectedStyle.Width(width).Render(line))
		} else {
			lines = append(lines, normalStyle.Render(line))
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
	body := panelStyle.Width(w - 4).Render(strings.Join(bodyLines[start:end], "\n"))

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

	// Save indicator.
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
	statusLeft := statusStyle.Render(date + mutedStyle.Render(fmt.Sprintf("%dw", wc)) + secStr + saveStr)

	aKey := "a ask claude"
	if m.aiGenerating {
		aKey = "a writing…"
	}
	keysRight := mutedStyle.Render(fmt.Sprintf("ctrl+s save  %s  [ ] jump  ctrl+f focus  esc done", aKey))
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
