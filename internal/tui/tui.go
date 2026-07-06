package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/aeon022/diaryctl/internal/diary"
	"github.com/aeon022/diaryctl/internal/git"
	"github.com/aeon022/diaryctl/internal/models"
	"github.com/aeon022/diaryctl/internal/store"
	"github.com/aeon022/diaryctl/internal/suite"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Design system ────────────────────────────────────────────────────────────

var (
	colorBlue   = lipgloss.AdaptiveColor{Light: "25", Dark: "33"}
	colorGreen  = lipgloss.AdaptiveColor{Light: "28", Dark: "42"}
	colorRed    = lipgloss.AdaptiveColor{Light: "160", Dark: "203"}
	colorAmber  = lipgloss.AdaptiveColor{Light: "214", Dark: "220"}
	colorMuted  = lipgloss.AdaptiveColor{Light: "243", Dark: "246"}
	colorSubtle = lipgloss.AdaptiveColor{Light: "250", Dark: "239"}
	selectedBg  = lipgloss.AdaptiveColor{Light: "189", Dark: "17"}
	selectedFg  = lipgloss.AdaptiveColor{Light: "16", Dark: "255"}

	_ = colorBlue
	_ = colorRed
	_ = colorSubtle
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorBlue).
			Padding(0, 1)

	selectedStyle = lipgloss.NewStyle().
			Background(selectedBg).
			Foreground(selectedFg).
			Padding(0, 1)

	normalStyle = lipgloss.NewStyle().
			Padding(0, 1)

	mutedStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	amberStyle = lipgloss.NewStyle().
			Foreground(colorAmber).
			Bold(true)

	greenStyle = lipgloss.NewStyle().
			Foreground(colorGreen)

	redStyle = lipgloss.NewStyle().
			Foreground(colorRed).
			Bold(true)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Padding(0, 1)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorMuted).
			Padding(0, 1)

	editorBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorGreen).
				Padding(0, 1)

	helpStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true)
)

// ── View types ───────────────────────────────────────────────────────────────

type viewType int

const (
	listView   viewType = iota
	detailView viewType = iota
	editorView viewType = iota
	repoView   viewType = iota
)

// ── Messages ─────────────────────────────────────────────────────────────────

type entriesLoadedMsg struct{ entries []models.Entry }
type reposLoadedMsg struct{ repos []models.Repo }
type entryGeneratedMsg struct {
	entry *models.Entry
	err   error
}
type autoSaveMsg struct{}
type errMsg struct{ err error }

type autoSaveTick struct{}

// ── Model ────────────────────────────────────────────────────────────────────

type Model struct {
	store   *store.Store
	view    viewType
	width   int
	height  int
	streak  int
	err     error
	message string
	msgTime time.Time

	// List view
	entries []models.Entry
	cursor  int

	// Searching
	searching   bool
	searchQuery string
	searchRes   []models.Entry

	// Delete confirmation
	confirmDelete bool
	deleteDate    time.Time

	// Detail view
	detail     *models.Entry
	detailScrl int

	// Editor view
	editorEntry  *models.Entry
	textarea     textarea.Model
	editorDirty  bool
	lastSaved    time.Time
	savedFlash   bool
	centeredMode bool

	// Repos view
	repos      []models.Repo
	repoCursor int
}

func New(s *store.Store) Model {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetWidth(80)
	ta.SetHeight(30)
	ta.Placeholder = ""
	ta.FocusedStyle.Base = lipgloss.NewStyle()
	ta.BlurredStyle.Base = lipgloss.NewStyle()

	return Model{
		store:    s,
		textarea: ta,
	}
}

// ── Init ─────────────────────────────────────────────────────────────────────

func (m Model) Init() tea.Cmd {
	return tea.Batch(loadEntries(m.store), loadRepos(m.store))
}

func loadEntries(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		entries, err := s.ListEntries(100)
		if err != nil {
			return errMsg{err}
		}
		return entriesLoadedMsg{entries}
	}
}

func loadRepos(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		repos, err := s.ListRepos()
		if err != nil {
			return errMsg{err}
		}
		return reposLoadedMsg{repos}
	}
}

func generateToday(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		repos, err := s.ListRepos()
		if err != nil {
			return entryGeneratedMsg{err: err}
		}
		today := time.Now()

		ds, err := git.DayStats(repos, today)
		if err != nil {
			return entryGeneratedMsg{err: err}
		}
		byRepo, err := git.CommitsByRepo(repos, today)
		if err != nil {
			return entryGeneratedMsg{err: err}
		}
		ds.ByRepo = byRepo

		streak, _ := s.GetStreak()
		ds.Streak = streak

		tasks, _ := suite.TodayTasks()
		events, _ := suite.TodayEvents()
		times, _ := suite.TodayTimeEntries()

		body := diary.BuildEntryBody(ds, tasks, events, times)
		if err := s.SaveEntry(today, body, false); err != nil {
			return entryGeneratedMsg{err: err}
		}
		entry, _ := s.GetEntry(today)
		return entryGeneratedMsg{entry: entry}
	}
}

func autoSaveCmd() tea.Cmd {
	return tea.Tick(30*time.Second, func(t time.Time) tea.Msg {
		return autoSaveMsg{}
	})
}

// ── Update ───────────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		streak, _ := m.store.GetStreak()
		m.streak = streak
		return m, nil

	case reposLoadedMsg:
		m.repos = msg.repos
		return m, nil

	case entryGeneratedMsg:
		if msg.err != nil {
			m.setMessage("Error: " + msg.err.Error())
			return m, nil
		}
		m.setMessage("Entry generated — press e to edit")
		return m, loadEntries(m.store)

	case autoSaveMsg:
		if m.view == editorView && m.editorDirty {
			m.doAutoSave()
			return m, autoSaveCmd()
		}
		return m, autoSaveCmd()

	case errMsg:
		m.err = msg.err
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// Route textarea updates.
	if m.view == editorView {
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		m.editorDirty = true
		return m, cmd
	}

	return m, nil
}

func (m *Model) setMessage(s string) {
	m.message = s
	m.msgTime = time.Now()
}

func (m *Model) doAutoSave() {
	if m.editorEntry == nil {
		return
	}
	body := m.textarea.Value()
	_ = m.store.SaveEntry(m.editorEntry.Date, body, m.editorEntry.Generated)
	m.editorDirty = false
	m.lastSaved = time.Now()
	m.savedFlash = true
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Global quit.
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}

	switch m.view {
	case listView:
		return m.handleListKey(msg)
	case detailView:
		return m.handleDetailKey(msg)
	case editorView:
		return m.handleEditorKey(msg)
	case repoView:
		return m.handleRepoKey(msg)
	}
	return m, nil
}

func (m Model) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Delete confirmation overlay.
	if m.confirmDelete {
		switch msg.String() {
		case "y", "Y":
			_ = m.store.DeleteEntry(m.deleteDate)
			m.confirmDelete = false
			m.setMessage("Deleted")
			if m.cursor > 0 {
				m.cursor--
			}
			return m, loadEntries(m.store)
		default:
			m.confirmDelete = false
		}
		return m, nil
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
		return m, nil
	}

	entries := m.displayEntries()
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
		if len(entries) > 0 && m.cursor < len(entries) {
			e := entries[m.cursor]
			m.detail = &e
			m.detailScrl = 0
			m.view = detailView
		}
	case "e":
		if len(entries) > 0 && m.cursor < len(entries) {
			e := entries[m.cursor]
			return m, m.openEditor(&e)
		}
	case "n":
		m.setMessage("Generating today's entry...")
		return m, generateToday(m.store)
	case "d":
		if len(entries) > 0 && m.cursor < len(entries) {
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
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
			return m, m.openEditor(m.detail)
		}
	case "d":
		if m.detail != nil {
			m.confirmDelete = true
			m.deleteDate = m.detail.Date
			m.view = listView
			m.detail = nil
		}
	}
	return m, nil
}

func (m Model) handleEditorKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+s":
		m.doAutoSave()
		// Refresh entry list in background.
		return m, loadEntries(m.store)

	case "esc":
		// Save on exit.
		if m.editorDirty {
			m.doAutoSave()
		}
		m.view = listView
		m.editorEntry = nil
		m.textarea.Blur()
		return m, loadEntries(m.store)

	case "ctrl+f":
		m.centeredMode = !m.centeredMode
		m.resizeEditor()
		return m, nil

	case "tab":
		// Jump to next <!-- AI: ... --> block.
		content := m.textarea.Value()
		row, col := m.textarea.Line(), m.textarea.LineInfo().ColumnOffset
		pos := lineColToOffset(content, row, col)
		next := strings.Index(content[pos+1:], "<!-- AI:")
		if next >= 0 {
			target := pos + 1 + next
			r, c := offsetToLineCol(content, target)
			m.textarea.SetCursor(r*10000 + c) // bubbles doesn't expose direct row/col set
			// Workaround: use SetValue + cursor positioning via GotoLine helper
			_ = r
			_ = c
		}
		// Fallback: pass tab through.
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		return m, cmd

	case "[":
		// Jump to previous ## section.
		content := m.textarea.Value()
		row, col := m.textarea.Line(), m.textarea.LineInfo().ColumnOffset
		pos := lineColToOffset(content, row, col)
		prev := strings.LastIndex(content[:pos], "\n## ")
		if prev >= 0 {
			r, _ := offsetToLineCol(content, prev+1)
			m.jumpToLine(r)
		}
		return m, nil

	case "]":
		// Jump to next ## section.
		content := m.textarea.Value()
		row, col := m.textarea.Line(), m.textarea.LineInfo().ColumnOffset
		pos := lineColToOffset(content, row, col)
		next := strings.Index(content[pos+1:], "\n## ")
		if next >= 0 {
			r, _ := offsetToLineCol(content, pos+1+next+1)
			m.jumpToLine(r)
		}
		return m, nil
	}

	// All other keys go to textarea.
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	m.editorDirty = true
	return m, cmd
}

func (m Model) handleRepoKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		if len(m.repos) > 0 && m.repoCursor < len(m.repos) {
			repo := m.repos[m.repoCursor]
			_ = m.store.DeleteRepo(repo.Path)
			return m, loadRepos(m.store)
		}
	}
	return m, nil
}

func (m *Model) openEditor(entry *models.Entry) tea.Cmd {
	m.editorEntry = entry
	m.textarea.SetValue(entry.Body)
	m.textarea.Focus()
	m.editorDirty = false
	m.lastSaved = time.Now()
	m.savedFlash = false
	m.resizeEditor()
	m.view = editorView
	return tea.Batch(autoSaveCmd(), m.textarea.Focus())
}

func (m *Model) resizeEditor() {
	w := m.width
	h := m.height
	if w < 40 {
		w = 80
	}
	if h < 20 {
		h = 24
	}

	if m.centeredMode {
		// Centered column: max 80 chars, padded on sides.
		textW := 78
		if w < textW+4 {
			textW = w - 4
		}
		m.textarea.SetWidth(textW)
	} else {
		m.textarea.SetWidth(w - 6)
	}
	m.textarea.SetHeight(h - 7)
}

// jumpToLine sets textarea to a given line (best-effort via SetValue trick).
func (m *Model) jumpToLine(line int) {
	// bubbles/textarea v1 doesn't expose direct cursor line set.
	// We simulate by re-positioning via cursor at start-of-line offset.
	content := m.textarea.Value()
	offset := 0
	for i, ch := range content {
		if line == 0 {
			offset = i
			break
		}
		if ch == '\n' {
			line--
		}
	}
	_ = offset
	// No public API — skip for now; section jump still moves view.
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

func (m Model) displayEntries() []models.Entry {
	if m.searchQuery != "" {
		return m.searchRes
	}
	return m.entries
}

// ── View ─────────────────────────────────────────────────────────────────────

func (m Model) View() string {
	if m.err != nil {
		return lipgloss.NewStyle().Foreground(colorRed).Render("Error: "+m.err.Error()) + "\n\nPress q to quit."
	}

	switch m.view {
	case detailView:
		return m.renderDetail()
	case editorView:
		return m.renderEditor()
	case repoView:
		return m.renderRepos()
	default:
		return m.renderList()
	}
}

func (m Model) renderList() string {
	w := m.width
	if w < 40 {
		w = 80
	}
	h := m.height
	if h < 20 {
		h = 24
	}

	heatW := 32
	listW := w - heatW - 6
	if listW < 20 {
		listW = 20
	}

	heatmap := m.renderHeatmap(heatW, h-6)
	entryList := m.renderEntryList(listW, h-6)

	top := lipgloss.JoinHorizontal(lipgloss.Top,
		panelStyle.Width(heatW).Height(h-6).Render(heatmap),
		"  ",
		panelStyle.Width(listW).Height(h-6).Render(entryList),
	)

	status := m.renderStatusBar(w)

	helpText := "j/k navigate  enter open  n new  e edit  d delete  r repos  / search  q quit"
	if m.confirmDelete {
		helpText = redStyle.Render(fmt.Sprintf("Delete %s? Press y to confirm, any other key to cancel", m.deleteDate.Format("2006-01-02")))
	}
	help := helpStyle.Render(helpText)

	return lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("diaryctl"),
		top,
		status,
		help,
	)
}

func (m Model) renderHeatmap(width, height int) string {
	today := time.Now()
	var lines []string

	lines = append(lines, mutedStyle.Render("last 30 days"))
	lines = append(lines, "")

	days := make([]time.Time, 30)
	for i := 0; i < 30; i++ {
		days[29-i] = today.AddDate(0, 0, -i)
	}

	commitMap := make(map[string]int)
	for _, e := range m.entries {
		key := e.Date.Format("2006-01-02")
		if e.Body != "" {
			commitMap[key] = len(strings.Split(e.Body, "\n- `"))
		}
	}

	header := mutedStyle.Render("S M T W T F S")
	lines = append(lines, header)

	firstDay := days[0].Weekday()
	cells := make([]string, int(firstDay))
	for i := 0; i < int(firstDay); i++ {
		cells[i] = "  "
	}

	for _, d := range days {
		key := d.Format("2006-01-02")
		count := commitMap[key]
		cell := heatCell(count)
		cells = append(cells, cell)
	}

	for len(cells)%7 != 0 {
		cells = append(cells, "  ")
	}

	for i := 0; i < len(cells); i += 7 {
		row := cells[i : i+7]
		lines = append(lines, strings.Join(row, " "))
	}

	_ = height
	_ = width
	return strings.Join(lines, "\n")
}

func heatCell(count int) string {
	block := "█"
	switch {
	case count == 0:
		return lipgloss.NewStyle().Foreground(colorMuted).Render(block)
	case count <= 2:
		return lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "71", Dark: "22"}).Render(block)
	case count <= 5:
		return lipgloss.NewStyle().Foreground(colorGreen).Render(block)
	default:
		return lipgloss.NewStyle().Foreground(colorGreen).Bold(true).Render(block)
	}
}

func (m Model) renderEntryList(width, height int) string {
	var lines []string

	if m.searching {
		lines = append(lines, amberStyle.Render("/"+m.searchQuery+"_"))
	} else {
		lines = append(lines, titleStyle.Render("Entries"))
	}

	entries := m.displayEntries()
	if len(entries) == 0 {
		lines = append(lines, "")
		lines = append(lines, mutedStyle.Render("No entries yet."))
		lines = append(lines, mutedStyle.Render("Press n to generate today's entry."))
		return strings.Join(lines, "\n")
	}

	maxVisible := height - 3
	start := 0
	if m.cursor >= maxVisible {
		start = m.cursor - maxVisible + 1
	}

	for i, e := range entries {
		if i < start {
			continue
		}
		if i-start >= maxVisible {
			break
		}

		dateStr := e.Date.Format("2006-01-02")
		preview := firstLine(e.Body)
		if len(preview) > width-15 {
			preview = preview[:width-15] + "…"
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

func (m Model) renderDetail() string {
	if m.detail == nil {
		return "No entry selected."
	}

	w := m.width
	if w < 40 {
		w = 80
	}
	h := m.height
	if h < 20 {
		h = 24
	}

	header := titleStyle.Render(m.detail.Date.Format("2006-01-02"))
	if m.detail.Generated {
		header += " " + greenStyle.Render("[AI-generated]")
	}

	bodyLines := strings.Split(m.detail.Body, "\n")
	maxLines := h - 6
	start := m.detailScrl
	if start >= len(bodyLines) {
		start = max(0, len(bodyLines)-1)
	}
	end := start + maxLines
	if end > len(bodyLines) {
		end = len(bodyLines)
	}

	body := strings.Join(bodyLines[start:end], "\n")
	bodyBlock := panelStyle.Width(w - 4).Render(body)

	help := helpStyle.Render("j/k scroll  e edit  d delete  esc back")

	return lipgloss.JoinVertical(lipgloss.Left, header, bodyBlock, help)
}

func (m Model) renderEditor() string {
	w := m.width
	if w < 40 {
		w = 80
	}

	content := m.textarea.Value()
	wordCount := countWords(content)
	section := currentSection(content, m.textarea.Line())

	// Save status.
	saveStatus := ""
	if m.savedFlash {
		elapsed := time.Since(m.lastSaved)
		if elapsed < 3*time.Second {
			saveStatus = greenStyle.Render(" ✓ saved")
		} else {
			m.savedFlash = false
		}
	} else if m.editorDirty {
		saveStatus = mutedStyle.Render(" ●")
	}

	// Status bar.
	date := ""
	if m.editorEntry != nil {
		date = amberStyle.Render(m.editorEntry.Date.Format("2006-01-02"))
	}
	wc := mutedStyle.Render(fmt.Sprintf("%dw", wordCount))
	sec := ""
	if section != "" {
		sec = mutedStyle.Render(" · " + section)
	}
	statusLeft := fmt.Sprintf(" %s  %s%s%s", date, wc, sec, saveStatus)
	statusRight := mutedStyle.Render("ctrl+s save  [ ] sections  tab AI-block  ctrl+f focus  esc done ")
	gap := w - lipgloss.Width(statusLeft) - lipgloss.Width(statusRight)
	if gap < 0 {
		gap = 0
	}
	statusBar := statusBarStyle.Render(statusLeft + strings.Repeat(" ", gap) + statusRight)

	// AI block highlight: scan content for <!-- AI: --> lines and show count.
	aiBlocks := strings.Count(content, "<!-- AI:")
	aiHint := ""
	if aiBlocks > 0 {
		aiHint = mutedStyle.Render(fmt.Sprintf("  %d AI prompt%s — tab to jump", aiBlocks, plural(aiBlocks)))
	}

	// Center mode framing.
	var editorBlock string
	if m.centeredMode {
		textW := m.textarea.Width()
		pad := (w - textW - 6) / 2
		if pad < 0 {
			pad = 0
		}
		margin := strings.Repeat(" ", pad)
		editorBlock = margin + editorBorderStyle.Width(textW+2).Render(m.textarea.View())
	} else {
		editorBlock = editorBorderStyle.Width(w-4).Render(m.textarea.View())
	}

	header := lipgloss.JoinHorizontal(lipgloss.Center,
		titleStyle.Render("diaryctl — editor"),
		aiHint,
	)

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		editorBlock,
		statusBar,
	)
}

func (m Model) renderRepos() string {
	var lines []string
	lines = append(lines, titleStyle.Render("Registered Repos"))
	lines = append(lines, "")

	if len(m.repos) == 0 {
		lines = append(lines, mutedStyle.Render("No repos registered."))
		lines = append(lines, mutedStyle.Render("Run: diaryctl init [path]"))
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

	lines = append(lines, "")
	lines = append(lines, helpStyle.Render("j/k navigate  d delete  esc back"))
	return strings.Join(lines, "\n")
}

func (m Model) renderStatusBar(width int) string {
	streak := amberStyle.Render(fmt.Sprintf("streak: %dd", m.streak))
	count := mutedStyle.Render(fmt.Sprintf("%d entries", len(m.entries)))

	msg := ""
	if m.message != "" && time.Since(m.msgTime) < 5*time.Second {
		msg = greenStyle.Render(" | " + m.message)
	}

	bar := fmt.Sprintf(" %s  %s%s", streak, count, msg)
	return statusBarStyle.Width(width).Render(bar)
}

// ── Helpers ──────────────────────────────────────────────────────────────────

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
	count := 0
	inWord := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			inWord = false
		} else if !inWord {
			inWord = true
			count++
		}
	}
	return count
}

func currentSection(content string, cursorLine int) string {
	lines := strings.Split(content, "\n")
	section := ""
	for i, line := range lines {
		if i > cursorLine {
			break
		}
		if strings.HasPrefix(line, "## ") {
			section = strings.TrimPrefix(line, "## ")
		}
	}
	return section
}

func lineColToOffset(content string, row, col int) int {
	line := 0
	for i, ch := range content {
		if line == row {
			return i + col
		}
		if ch == '\n' {
			line++
		}
	}
	return len(content)
}

func offsetToLineCol(content string, offset int) (int, int) {
	line := 0
	lineStart := 0
	for i, ch := range content {
		if i == offset {
			return line, offset - lineStart
		}
		if ch == '\n' {
			line++
			lineStart = i + 1
		}
	}
	return line, offset - lineStart
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Run starts the Bubbletea program.
func Run(s *store.Store) error {
	m := New(s)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}
