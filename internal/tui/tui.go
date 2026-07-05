package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/aeon022/diaryctl/internal/diary"
	"github.com/aeon022/diaryctl/internal/models"
	"github.com/aeon022/diaryctl/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- Design system colors (shared across suite) ---

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

// --- Styles ---

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

	statusBarStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Padding(0, 1)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorMuted).
			Padding(0, 1)

	helpStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true)
)

// --- View types ---

type viewType int

const (
	listView   viewType = iota
	detailView viewType = iota
	repoView   viewType = iota
	searchView viewType = iota
)

// --- Messages ---

type entriesLoadedMsg struct {
	entries []models.Entry
}

type reposLoadedMsg struct {
	repos []models.Repo
}

type entryGeneratedMsg struct {
	entry *models.Entry
	err   error
}

type errMsg struct{ err error }

// --- Model ---

// Model is the top-level Bubbletea model.
type Model struct {
	store   *store.Store
	view    viewType
	width   int
	height  int
	streak  int
	err     error
	message string

	// List view
	entries []models.Entry
	cursor  int
	offset  int

	// Detail view
	detail     *models.Entry
	detailScrl int

	// Repos view
	repos      []models.Repo
	repoCursor int

	// Search
	searching   bool
	searchQuery string
	searchRes   []models.Entry
}

// New creates a new TUI model.
func New(s *store.Store) Model {
	return Model{
		store: s,
	}
}

// --- Init ---

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
		body, err := diary.BuildEntryBody(repos, today, s)
		if err != nil {
			return entryGeneratedMsg{err: err}
		}
		if err := s.SaveEntry(today, body, false); err != nil {
			return entryGeneratedMsg{err: err}
		}
		entry, _ := s.GetEntry(today)
		return entryGeneratedMsg{entry: entry}
	}
}

// --- Update ---

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
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
			m.message = "Error: " + msg.err.Error()
			return m, nil
		}
		m.message = "Entry generated for today"
		// Refresh entries and open detail.
		return m, tea.Batch(loadEntries(m.store), func() tea.Msg {
			return entriesLoadedMsg{}
		})

	case errMsg:
		m.err = msg.err
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Global keys.
	switch msg.String() {
	case "ctrl+c", "q":
		if m.view == listView && !m.searching {
			return m, tea.Quit
		}
	}

	switch m.view {
	case listView:
		return m.handleListKey(msg)
	case detailView:
		return m.handleDetailKey(msg)
	case repoView:
		return m.handleRepoKey(msg)
	}
	return m, nil
}

func (m Model) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.searching {
		switch msg.String() {
		case "esc", "enter":
			m.searching = false
			if msg.String() == "enter" {
				m.filterEntries()
			}
		case "backspace":
			if len(m.searchQuery) > 0 {
				m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
				m.filterEntries()
			}
		default:
			m.searchQuery += msg.String()
			m.filterEntries()
		}
		return m, nil
	}

	switch msg.String() {
	case "j", "down":
		m.cursor++
		if m.cursor >= len(m.displayEntries()) {
			m.cursor = len(m.displayEntries()) - 1
		}
	case "k", "up":
		m.cursor--
		if m.cursor < 0 {
			m.cursor = 0
		}
	case "enter":
		entries := m.displayEntries()
		if len(entries) > 0 && m.cursor < len(entries) {
			m.detail = &entries[m.cursor]
			m.detailScrl = 0
			m.view = detailView
		}
	case "n":
		m.message = "Generating today's entry..."
		return m, generateToday(m.store)
	case "e":
		entries := m.displayEntries()
		if len(entries) > 0 && m.cursor < len(entries) {
			entry := entries[m.cursor]
			return m, openInEditor(m.store, &entry)
		}
	case "r":
		m.view = repoView
		m.repoCursor = 0
	case "v":
		// Stats view — placeholder, handled by stats command.
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
			return m, openInEditor(m.store, m.detail)
		}
	}
	return m, nil
}

func (m Model) handleRepoKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.view = listView
	case "j", "down":
		m.repoCursor++
		if m.repoCursor >= len(m.repos) {
			m.repoCursor = len(m.repos) - 1
		}
	case "k", "up":
		m.repoCursor--
		if m.repoCursor < 0 {
			m.repoCursor = 0
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

func openInEditor(s *store.Store, entry *models.Entry) tea.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	// Write to a temp file.
	tmpFile, err := os.CreateTemp("", "diaryctl-*.md")
	if err != nil {
		return func() tea.Msg { return errMsg{err} }
	}
	tmpFile.WriteString(entry.Body)
	tmpFile.Close()

	return tea.ExecProcess(exec.Command(editor, tmpFile.Name()), func(err error) tea.Msg {
		if err != nil {
			return errMsg{err}
		}
		content, err := os.ReadFile(tmpFile.Name())
		os.Remove(tmpFile.Name())
		if err != nil {
			return errMsg{err}
		}
		_ = s.SaveEntry(entry.Date, string(content), entry.Generated)
		entries, _ := s.ListEntries(100)
		return entriesLoadedMsg{entries}
	})
}

// --- View ---

func (m Model) View() string {
	if m.err != nil {
		return lipgloss.NewStyle().Foreground(colorRed).Render("Error: "+m.err.Error()) + "\n\nPress q to quit."
	}

	switch m.view {
	case detailView:
		return m.renderDetail()
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

	// Split width: heatmap on left (~30%), entries on right.
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
	help := helpStyle.Render("j/k navigate  enter open  n new  e edit  r repos  / search  q quit")

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

	// Build 30 days, newest last.
	days := make([]time.Time, 30)
	for i := 0; i < 30; i++ {
		days[29-i] = today.AddDate(0, 0, -i)
	}

	// Count commits per day (approximate: check entry bodies).
	commitMap := make(map[string]int)
	for _, e := range m.entries {
		key := e.Date.Format("2006-01-02")
		if e.Body != "" {
			commitMap[key] = len(strings.Split(e.Body, "\n- `"))
		}
	}

	// Render in rows of 7 (week columns).
	// Header: days of week.
	header := mutedStyle.Render("S M T W T F S")
	lines = append(lines, header)

	// Find start offset (what day of week does the first day fall on?).
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

	// Pad to multiple of 7.
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
		start = len(bodyLines) - 1
	}
	end := start + maxLines
	if end > len(bodyLines) {
		end = len(bodyLines)
	}

	body := strings.Join(bodyLines[start:end], "\n")
	bodyBlock := panelStyle.Width(w - 4).Render(body)

	help := helpStyle.Render("j/k scroll  e edit  esc back")

	return lipgloss.JoinVertical(lipgloss.Left, header, bodyBlock, help)
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
	if m.message != "" {
		msg = greenStyle.Render(" | " + m.message)
	}

	bar := fmt.Sprintf(" %s  %s%s", streak, count, msg)
	return statusBarStyle.Width(width).Render(bar)
}

// --- Helpers ---

func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "<!--") {
			// strip markdown formatting
			line = strings.TrimPrefix(line, "- ")
			line = strings.TrimPrefix(line, "* ")
			return line
		}
	}
	return "(empty)"
}

// Run starts the Bubbletea program.
func Run(s *store.Store) error {
	m := New(s)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}
