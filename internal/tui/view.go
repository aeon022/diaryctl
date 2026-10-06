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
	"github.com/aeon022/missionctl-core/palette"
	"github.com/aeon022/missionctl-core/statusbar"
	"github.com/aeon022/missionctl-core/ui"
	"github.com/charmbracelet/x/ansi"
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
		return redStyle.Render("Error: "+m.err.Error()) + "\n\nPress q to quit."
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

	vp := viewport.New(viewport.WithWidth(popW-6), viewport.WithHeight(popH-5)) // border 1+1, padding(1,2) → 2 rows/4 cols; -1 row for footer
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

// panelHeight is the shared content-based height for the heatmap and
// entries panels (they must match so the side-by-side boxes line up) —
// used by both viewList (rendering) and rowHitTest (hit-testing), so they
// can't drift apart on how tall the panels actually are.
func (m *Model) panelHeight() int {
	h := len(strings.Split(m.renderHeatmap(), "\n"))
	if h < 8 {
		h = 8
	}
	return h
}

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

	panelH := m.panelHeight()

	left := panelStyle.Width(heatW).Height(panelH).Render(m.renderHeatmap())
	// lipgloss v2: Width includes the panel border (2) and padding (2), so the
	// rows must be built for listW-4 cells or every row wraps inside the box.
	right := panelStyle.Width(listW).Height(panelH).Render(m.renderEntryList(listW-4, panelH))
	top := lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)

	// One-line footer: contextual hints on the left (priority order, the last
	// ones drop first on a narrow terminal), the flash message on the right.
	hints := statusbar.Hints(w,
		[2]string{"enter", "open"}, [2]string{"n", "new"}, [2]string{"?", "help"}, [2]string{"q", "quit"},
		[2]string{"j/k", "move"}, [2]string{"e", "edit"}, [2]string{"d", "delete"}, [2]string{"u", "undo"},
		[2]string{"/", "search"}, [2]string{"y", "copy"}, [2]string{"g", "note"}, [2]string{"r", "repos"})
	if m.confirmDelete {
		hints = redStyle.Render(fmt.Sprintf(
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
		msg = greenStyle.Render(m.message)
	}
	footer := statusbar.Line(w, hints, msg)

	// Header: title left, "<n> entries · streak Nd · today …" in the middle
	// (streak keeps its pulsing flame above 7 days), date right. This replaces
	// the old Today / Recent Entries / streak lines under the panels — the
	// recent entries just repeated the list above.
	streakStr := amberStyle.Render(fmt.Sprintf("streak %dd", m.streak))
	if m.streak > 7 {
		flame := amberStyle.Render("🔥 ")
		if m.tickCount%2 != 0 {
			flame = redStyle.Render("🔥 ")
		}
		streakStr = flame + streakStr
	}
	mid := mutedStyle.Render(fmt.Sprintf("%d entries", len(m.entries))) + mutedStyle.Render(" · ") + streakStr
	if today := m.todaySummary(); today != "" {
		mid += mutedStyle.Render(" · today ") + today
	}
	header := ui.Header(w, titleStyle.Render("diaryctl")+mutedStyle.Render("· Journal"), mid, mutedStyle.Render(time.Now().Format("Mon 02 Jan")))

	body := lipgloss.JoinVertical(lipgloss.Left, header, "", top)

	// Pin the footer to the bottom of the screen instead of letting it glue
	// itself right under the panels — pad the body out to the terminal
	// height first, same pattern taskctl/notectl use.
	for lines := strings.Count(body, "\n") + 1; lines < h-1; lines++ {
		body += "\n"
	}

	return body + "\n" + footer
}

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
		if m.loading {
			lines = append(lines, "", emptystate.Loading(0, 0, m.sp.View(), "Loading entries…"))
		} else {
			lines = append(lines, "", emptystate.Render(0, 0, "", "No entries yet", "press n to generate today's entry."))
		}
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

		title, tags := diary.ParseTitleTags(e.Body)
		preview := title
		if preview == "" {
			preview = firstLine(e.Body)
		}

		tagPlain := ""
		tagStyled := ""
		for _, t := range tags {
			tagPlain += " #" + t
			if diary.IsKnownCategory(t) {
				tagStyled += " " + categoryStyle.Render("#"+t)
			} else {
				tagStyled += " " + mutedStyle.Render("#"+t)
			}
		}
		if e.Generated {
			tagPlain += " [AI]"
			tagStyled += " " + greenStyle.Render("[AI]")
		}

		// Everything is measured in display cells (not bytes): the old byte
		// length plus an appended "…" made truncated titles one cell too wide,
		// so the row wrapped and "[AI]" fell onto the next line. Hashtags give
		// way to the title when the row is tight.
		maxP := rowW - 14 - lipgloss.Width(tagPlain)
		if maxP < 12 && len(tags) > 0 {
			tagPlain, tagStyled = "", ""
			if e.Generated {
				tagPlain, tagStyled = " [AI]", " "+greenStyle.Render("[AI]")
			}
			maxP = rowW - 14 - lipgloss.Width(tagPlain)
		}
		if maxP < 4 && tagPlain != "" { // no room for a readable title: the tag gives way too
			tagPlain, tagStyled = "", ""
			maxP = rowW - 14
		}
		if maxP < 0 {
			maxP = 0
		}
		preview = ansi.Truncate(preview, maxP, "…")
		preview += strings.Repeat(" ", max(maxP-lipgloss.Width(preview), 0)) // pad by cells, not runes

		switch {
		case i == m.cursor, i == m.hoverRow:
			// v2 Width includes the Padding(0,1): Width(rowW+2) = rowW content cells = width in total.
			rowText := fmt.Sprintf("%-12s  %s%s", dateStr, preview, tagPlain)
			if i == m.cursor {
				lines = append(lines, selectedStyle.Width(rowW+2).Render(rowText))
			} else {
				lines = append(lines, hoverStyle.Width(rowW+2).Render(rowText))
			}
		default:
			// Build styled row without nesting ANSI inside fmt.Sprintf — avoids
			// lipgloss width miscalculation on content with embedded escape codes.
			var previewStyled string
			switch {
			case m.searchQuery != "":
				previewStyled = highlightMatch(preview, m.searchQuery)
			case title != "":
				// A real title, not just a body-snippet preview — worth a
				// touch more visual weight than the muted fallback below.
				previewStyled = titleRowStyle.Render(preview)
			default:
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
	panelHeight := m.panelHeight()

	entries := m.visibleEntries()
	if len(entries) == 0 {
		return -1
	}
	maxVis := panelHeight - 3
	start := 0
	if m.cursor >= maxVis {
		start = m.cursor - maxVis + 1
	}

	// header(0) + blank(1) + panel top border(2) + title row(3) → entries start at 4.
	idx := y - 4
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
	w := m.width
	if w < 40 {
		w = 80
	}

	title, tags := diary.ParseTitleTags(m.detail.Body)
	header := m.renderHeader("Entry") + "  " + amberStyle.Render(m.detail.Date.Format("2006-01-02"))
	if title != "" {
		header += "  " + titleRowStyle.Render(title)
	}
	for _, t := range tags {
		if diary.IsKnownCategory(t) {
			header += " " + categoryStyle.Render("#"+t)
		} else {
			header += " " + mutedStyle.Render("#"+t)
		}
	}
	if m.detail.Generated {
		header += " " + greenStyle.Render("[AI]")
	}

	body := panelStyle.Width(w - 4).Render(m.detailVP.View())

	scroll := ""
	if m.detailVP.TotalLineCount() > m.detailVP.Height() {
		scroll = mutedStyle.Render(fmt.Sprintf("%d%%", int(m.detailVP.ScrollPercent()*100)))
	}
	footer := statusbar.Line(w, statusbar.Hints(w-lipgloss.Width(scroll)-1,
		[2]string{"esc", "back"}, [2]string{"j/k", "scroll"}, [2]string{"e", "edit"},
		[2]string{"d", "delete"}, [2]string{"g", "open note"}), scroll)

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		body,
		footer,
	)
}

func (m *Model) viewEditor() string {
	w := m.width
	if w < 40 {
		w = 80
	}

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

	aKey := "ctrl+g ask claude"
	if m.aiGenerating {
		aKey = "ctrl+g writing…"
	}
	vimHint := "ctrl+v vim"
	if m.vimNormal {
		vimHint = "i insert  hjkl move"
	}
	keysRight := mutedStyle.Render(fmt.Sprintf("ctrl+s save  %s  [ ] jump  ctrl+f focus  %s  esc done", aKey, vimHint))
	gap := w - lipgloss.Width(statusLeft) - lipgloss.Width(keysRight)
	if gap < 1 {
		// statusLeft (word count/timer/save state) is the core status —
		// drop the key-hint text instead of clamping gap to 1 and
		// appending it anyway, which could push the line past w.
		keysRight = ""
		gap = w - lipgloss.Width(statusLeft)
		if gap < 0 {
			gap = 0
		}
	}
	statusBar := statusLeft + strings.Repeat(" ", gap) + keysRight

	aiHint := ""
	if m.aiGenerating {
		dots := [4]string{"⠋", "⠙", "⠹", "⠸"}
		spin := dots[time.Now().UnixMilli()/120%4]
		aiHint = "  " + amberStyle.Render(fmt.Sprintf("%s AI writing… %d words", spin, m.aiTokens))
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
		lines = append(lines, emptystate.Render(0, 0, "", "No repos registered", "run: diaryctl init [path]"))
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
		footer = statusbar.Hints(m.width, [2]string{"esc", "back"}, [2]string{"j/k", "navigate"},
			[2]string{"d", "delete"}, [2]string{"u", "undo"})
	}
	lines = append(lines, "")

	// Pin the footer to the bottom of the screen instead of letting it
	// glue itself right under a short repo list — pad the body out to
	// the terminal height first, same pattern taskctl/notectl use.
	for len(lines) < m.height-1 {
		lines = append(lines, "")
	}

	lines = append(lines, footer)
	return strings.Join(lines, "\n")
}

func (m *Model) renderActAskPopup() string {
	body := fmt.Sprintf("Add today's activity (%d events) to your diary?\n\n", m.actAskCount) +
		mutedStyle.Render("y add now  ·  n not now  ·  a always  ·  x never")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBlue).
		Padding(1, 2).
		Render(body)
}
