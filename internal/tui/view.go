package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/aeon022/diaryctl/internal/diary"
	"github.com/aeon022/missionctl-core/emptystate"
	"github.com/aeon022/missionctl-core/keymap"
	"github.com/aeon022/missionctl-core/overlay"
	"github.com/aeon022/missionctl-core/statusbar"
	"github.com/aeon022/missionctl-core/ui"
)

// ── View ──────────────────────────────────────────────────────────────────────

func (m *Model) View() tea.View {
	v := tea.NewView(m.viewContent())
	// v1's tea.WithAltScreen()/WithMouseAllMotion() Program options are gone
	// in v2 — they are per-View fields now.
	v.AltScreen = true
	v.MouseMode = tea.MouseModeAllMotion
	v.ReportFocus = true // FocusMsg → reload when the window regains focus
	return v
}

func (m *Model) viewContent() string {
	if m.err != nil {
		return m.chrome("Error", "", "Error", redStyle.Render(m.err.Error()),
			statusbar.Line(m.geom().w, statusbar.Hints(m.geom().w, [2]string{"q", "quit"}), ""), true)
	}
	if m.actAsk && m.view == listView {
		return overlay.CenterDim(m.viewList(), m.renderActAskPopup(), m.width, m.height, 0)
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
		return overlay.CenterDim(m.viewList(), m.renderHelpPopup(), m.width, m.height, 0)
	default:
		return m.viewList()
	}
}

// chrome is the shared frame of every secondary view: header (diaryctl ·
// <section>, <mid>, date) + divider (+ blank line in the tall tier), one
// titled panel around body, and a one-line footer — exactly the terminal
// height (ui.Frame), like the main list.
func (m *Model) chrome(section, mid, panelTitle, body, footer string, focused bool) string {
	g := m.geom()
	header := ui.Header(g.w, titleStyle.Render("diaryctl")+mutedStyle.Render("· "+section), mid,
		mutedStyle.Render(time.Now().Format("Mon 02 Jan"))) + "\n" + ui.Divider(g.w, "")
	if g.spacious {
		header += "\n"
	}
	return ui.Frame(g.h, header, g.panel(g.w, g.bodyH, panelTitle, body, focused), footer)
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
		Row("ctrl+g", "ask AI to continue the entry").
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

	vp := viewport.New(viewport.WithWidth(popW-4), viewport.WithHeight(popH-3)) // ui.Panel: border 2 + 1 left pad; -1 row for the footer line
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
	if m.helpVP.TotalLineCount() > m.helpVP.Height() {
		footer = fmt.Sprintf("j/k scroll (%d%%)  ·  %s", int(m.helpVP.ScrollPercent()*100), footer)
	}
	body := m.helpVP.View() + "\n" + mutedStyle.Render(footer)
	return ui.Panel(m.helpPopW, m.helpPopH, "Help", body, true)
}

// doubleClickWindow opens the entry detail on a second click within this
// window, same pattern and duration taskctl uses for its own double-click.
const doubleClickWindow = 400 * time.Millisecond

// undoWindow is how long after a delete "u" still restores it — same
// duration taskctl uses for its own delete-undo.
const undoWindow = 5 * time.Second

// heatLevels is the shared 5-tier commit-count gradient, index 0 = none.
var heatLevels = [5]lipgloss.Style{
	mutedStyle,
	lipgloss.NewStyle().Foreground(adaptive("#86efac", "#276749")),
	lipgloss.NewStyle().Foreground(adaptive("#4ade80", "#38a169")),
	lipgloss.NewStyle().Foreground(adaptive("#22c55e", "#48bb78")),
	lipgloss.NewStyle().Foreground(adaptive("#16a34a", "#68d391")),
}

func heatLevel(cnt int) int {
	switch {
	case cnt <= 0:
		return 0
	case cnt <= 2:
		return 1
	case cnt <= 5:
		return 2
	case cnt <= 9:
		return 3
	default:
		return 4
	}
}

// renderHeatmap draws a GitHub-style contribution graph: weeks run
// horizontally (one column each) with a row per weekday, so the covered
// period scales with the panel's fixed width without growing taller —
// unlike the old one-row-per-week layout, which needed a new line for
// every 7 days and forced either a short window or a tall panel. Same
// technique habctl's yearly heatmap already uses.
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

	// Fit as many weeks as the panel's fixed content width allows: border(2)
	// + padding(2) leaves heatmapPanelW-4 columns, minus a 2-char day-label
	// gutter, 2 columns ("█ ") per week.
	contentW := heatmapPanelW - 4
	weeks := (contentW - 2) / 2
	if weeks > 52 {
		weeks = 52
	}
	if weeks < 4 {
		weeks = 4
	}

	wd := int(today.Weekday())
	daysFromMonday := (wd + 6) % 7
	thisMonday := today.AddDate(0, 0, -daysFromMonday)
	startDate := thisMonday.AddDate(0, 0, -(weeks-1)*7)

	var monthLine strings.Builder
	monthLine.WriteString("  ")
	lastMonth := -1
	for w := 0; w < weeks; w++ {
		day := startDate.AddDate(0, 0, w*7)
		mo := int(day.Month())
		if mo != lastMonth {
			monthLine.WriteString(mutedStyle.Render(day.Format("Jan")[:1]))
			lastMonth = mo
		} else {
			monthLine.WriteString(" ")
		}
		monthLine.WriteString(" ")
	}

	dayLabels := []string{"M", "T", "W", "T", "F", "S", "S"}
	var lines []string
	lines = append(lines, mutedStyle.Render(fmt.Sprintf("last %d weeks", weeks)))
	lines = append(lines, monthLine.String())
	for d := 0; d < 7; d++ {
		var row strings.Builder
		row.WriteString(mutedStyle.Render(dayLabels[d]) + " ")
		for w := 0; w < weeks; w++ {
			day := startDate.AddDate(0, 0, w*7+d)
			if day.After(today) {
				row.WriteString("  ")
				continue
			}
			key := day.Format("2006-01-02")
			if key == selectedKey {
				row.WriteString(amberStyle.Bold(true).Render("█") + " ")
				continue
			}
			level := heatLevel(commitMap[key])
			cell := "░"
			if level > 0 {
				cell = "█"
			}
			row.WriteString(heatLevels[level].Render(cell) + " ")
		}
		lines = append(lines, row.String())
	}
	lines = append(lines, "")
	lines = append(lines, mutedStyle.Render("0 ")+
		heatLevels[1].Render("█")+mutedStyle.Render(" 1-2 ")+
		heatLevels[2].Render("█")+mutedStyle.Render(" 3-5 ")+
		heatLevels[3].Render("█")+mutedStyle.Render(" 6-9 ")+
		heatLevels[4].Render("█")+mutedStyle.Render(" 10+"))

	return strings.Join(lines, "\n")
}

// renderTodayLine is a standalone, prominent summary of today's suite
// activity — pulled out of the heatmap panel (where it used to be tucked
// away as a small aside) into its own full-width line.
// todaySummary is today's activity as one styled phrase ("3 commits · 2 tasks"),
// "" until the suite data has loaded.
func (m *Model) todaySummary() string {
	if !m.todayLoaded {
		return ""
	}
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
		parts = append(parts, diary.FormatDuration(m.todayDuration))
	}
	if len(parts) == 0 {
		return mutedStyle.Render("nothing yet")
	}
	return strings.Join(parts, mutedStyle.Render(" · "))
}

func (m *Model) viewDetail() string {
	if m.detail == nil {
		return "No entry selected."
	}
	g := m.geom()

	title, tags := diary.ParseTitleTags(m.detail.Body)
	mid := amberStyle.Render(m.detail.Date.Format("2006-01-02"))
	for _, t := range tags {
		if diary.IsKnownCategory(t) {
			mid += " " + categoryStyle.Render("#"+t)
		} else {
			mid += " " + mutedStyle.Render("#"+t)
		}
	}
	if m.detail.Generated {
		mid += " " + greenStyle.Render("AI")
	}
	if title == "" {
		title = "Entry"
	}

	scroll := ""
	if m.detailVP.TotalLineCount() > m.detailVP.Height() {
		scroll = mutedStyle.Render(fmt.Sprintf("%d%%", int(m.detailVP.ScrollPercent()*100)))
	}
	footer := statusbar.Line(g.w, statusbar.Hints(g.w-lipgloss.Width(scroll)-1,
		[2]string{"esc", "back"}, [2]string{"j/k", "scroll"}, [2]string{"e", "edit"},
		[2]string{"d", "delete"}, [2]string{"g", "open note"}), scroll)

	return m.chrome("Entry", mid, title, m.detailVP.View(), footer, true)
}

func (m *Model) viewEditor() string {
	g := m.geom()
	w := g.w

	content := m.ta.Value()
	wc := diary.WordCount(content)
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

	// Footer: the editor status on the left (always kept), key hints on the
	// right in priority order — `esc` first, the last ones drop when narrow.
	aiLabel := "ask AI"
	if m.aiGenerating {
		aiLabel = "writing…"
	}
	hints := [][2]string{{"esc", "save & close"}, {"ctrl+s", "save"}, {"ctrl+g", aiLabel}, {"ctrl+v", "vim"}, {"[ ]", "jump"}, {"ctrl+f", "focus"}}
	if m.vimNormal {
		hints = [][2]string{{"esc", "save & close"}, {"i", "insert"}, {"hjkl", "move"}, {"ctrl+s", "save"}}
	}
	footer := statusbar.Line(w, statusLeft, statusbar.Hints(max(w-lipgloss.Width(statusLeft)-2, 0), hints...))

	aiHint := ""
	if m.aiGenerating {
		dots := [4]string{"⠋", "⠙", "⠹", "⠸"}
		spin := dots[time.Now().UnixMilli()/120%4]
		aiHint = amberStyle.Render(fmt.Sprintf("%s AI writing… %d words", spin, m.aiTokens))
	} else if aiBlocks > 0 {
		aiHint = mutedStyle.Render(fmt.Sprintf("%d AI prompt%s · a to fill · tab to jump", aiBlocks, plural(aiBlocks)))
	}

	editor := m.ta.View()
	if m.centeredMode {
		pad := max((g.contentW(g.w)-m.ta.Width())/2, 0)
		margin := strings.Repeat(" ", pad)
		editor = margin + strings.ReplaceAll(editor, "\n", "\n"+margin)
	}
	return m.chrome("Editor", aiHint, "Entry", editor, footer, true)
}

func (m *Model) viewRepos() string {
	g := m.geom()
	cw := g.contentW(g.w)
	var lines []string
	if len(m.repos) == 0 {
		lines = append(lines, emptystate.Render(0, 0, "", "No repos registered", "run: diaryctl init [path]"))
	} else {
		for i, r := range m.repos {
			lines = append(lines, ui.Row(cw, i == m.repoCursor, fmt.Sprintf("%-20s %s", r.Name, r.Path)))
		}
	}
	var hints string
	right := ""
	switch {
	case m.confirmDeleteRepo && len(m.repos) > 0:
		hints = redStyle.Render(fmt.Sprintf(
			"Delete %s? y = confirm, any other key = cancel", m.repos[m.repoCursor].Name,
		))
	default:
		hints = statusbar.Hints(g.w, [2]string{"esc", "back"}, [2]string{"j/k", "navigate"},
			[2]string{"d", "delete"}, [2]string{"u", "undo"})
		if m.message != "" && time.Since(m.msgAt) < undoWindow {
			right = greenStyle.Render(m.message)
		}
	}
	mid := mutedStyle.Render(fmt.Sprintf("%d tracked", len(m.repos)))
	return m.chrome("Repos", mid, "Git repos", strings.Join(lines, "\n"), statusbar.Line(g.w, hints, right), true)
}

func (m *Model) renderActAskPopup() string {
	body := fmt.Sprintf("Add today's activity (%d events) to your diary?", m.actAskCount) + "\n\n" +
		mutedStyle.Render("y add now  ·  n not now  ·  a always  ·  x never")
	g := m.geom()
	return ui.Panel(min(g.w, 62), 6, "Activity", " \n"+body, true)
}
