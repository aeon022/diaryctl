package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/aeon022/diaryctl/internal/diary"
	"github.com/aeon022/diaryctl/internal/models"
	"github.com/aeon022/missionctl-core/emptystate"
	"github.com/aeon022/missionctl-core/palette"
	"github.com/aeon022/missionctl-core/statusbar"
	"github.com/aeon022/missionctl-core/theme"
	"github.com/aeon022/missionctl-core/ui"
	"github.com/charmbracelet/x/ansi"
)

// heatmapPanelW is the heatmap (left) panel's fixed outer width.
const heatmapPanelW = 32

// listGeom is the ONE description of the list view's layout: viewList draws
// from it and rowHitTest / digit-jump read it, so what is drawn and what a
// click hits can't drift apart.
type listGeom struct {
	w, h     int
	spacious bool // header block gets a blank line (height >= 30)
	headRows int  // header + divider (+ blank)
	bodyH    int  // rows between the header block and the one-line footer
	heat     bool // heatmap column shown (width >= 80)
	wide     bool // preview column shown (width >= 120)
	framed   bool // panels have borders (width >= 80; borders: none also drops them)
	listX    int  // x of the entries panel's left edge
	listW    int  // entries panel outer width
	prevX    int
	prevW    int
}

func (m *Model) geom() listGeom {
	g := listGeom{w: m.width, h: m.height}
	if g.w < 40 {
		g.w = 80
	}
	if g.h < 20 {
		g.h = 24
	}
	g.spacious = g.h >= 30
	g.headRows = 2
	if g.spacious {
		g.headRows = 3
	}
	g.bodyH = max(g.h-g.headRows-1, 3)
	g.heat = g.w >= 80
	g.wide = g.w >= 120
	g.framed = g.w >= 80 && ui.Borders() != ui.BorderNone
	g.listW = g.w
	switch {
	case g.wide:
		rest := g.w - heatmapPanelW - 2
		g.listX = heatmapPanelW + 1
		g.listW = max(44, rest*2/5)
		g.prevX = g.listX + g.listW + 1
		g.prevW = g.w - g.prevX
	case g.heat:
		g.listX = heatmapPanelW + 1
		g.listW = g.w - g.listX
	}
	return g
}

// contentW / contentH are the usable cells inside a panel of the given outer
// size: Panel keeps a 1-cell left pad inside its border (framed), or turns the
// top border into a divider line and drops the sides (unframed).
func (g listGeom) contentW(outer int) int {
	if g.framed {
		return max(outer-3, 1)
	}
	return outer
}

func (g listGeom) contentH(outer int) int {
	if g.framed {
		return max(outer-2, 1)
	}
	return max(outer-1, 1)
}

// panel draws a titled panel; below 80 columns (or with borders: none) it is
// a divider line with the title followed by the content, no frame.
func (g listGeom) panel(w, h int, title, content string, focused bool) string {
	if g.framed {
		return ui.Panel(w, h, title, content, focused)
	}
	lines := strings.Split(content, "\n")
	if len(lines) > h-1 {
		lines = lines[:max(h-1, 0)]
	}
	return ui.Divider(w, title) + "\n" + strings.Join(lines, "\n")
}

// listHeadLines are the lines above the entries: the command palette or the
// search prompt (none in the normal case — the panel title says "Entries").
func (m *Model) listHeadLines() []string {
	var lines []string
	switch {
	case m.inPalette:
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
	case m.searching:
		lines = append(lines, amberStyle.Render("/"+m.searchQuery+"_"))
	}
	return lines
}

// listWindow returns how many entry rows fit in contentH and the index of the
// first visible entry (the cursor is always kept on screen).
func (m *Model) listWindow(contentH int) (maxVis, start int) {
	maxVis = max(contentH-len(m.listHeadLines()), 1)
	if m.cursor >= maxVis {
		start = m.cursor - maxVis + 1
	}
	return maxVis, start
}

// listVisible is the number of entry rows currently on screen (digit keys
// 1-9 jump within it).
func (m *Model) listVisible() int {
	g := m.geom()
	maxVis, _ := m.listWindow(g.contentH(g.bodyH))
	return maxVis
}

// rowHitTest returns the visibleEntries() index at screen position (x, y), or
// -1. Rows start under the header block and the panel's top border/divider
// and the optional palette/search lines; x must be inside the entries panel.
func (m *Model) rowHitTest(x, y int) int {
	g := m.geom()
	if x < g.listX || x >= g.listX+g.listW {
		return -1
	}
	entries := m.visibleEntries()
	if len(entries) == 0 {
		return -1
	}
	maxVis, start := m.listWindow(g.contentH(g.bodyH))
	idx := y - (g.headRows + 1) - len(m.listHeadLines())
	if idx < 0 || idx >= maxVis {
		return -1
	}
	i := start + idx
	if i >= len(entries) {
		return -1
	}
	return i
}

// dateCol is the width of the date column plus its two-space gap.
const dateCol = 12

// entryRow draws one entry as exactly cw cells: dim date, title, dimmed
// hashtags and a small AI tag at the END. The title is truncated first so the
// tag can never wrap; hashtags and then the tag give way on a tight row.
func (m *Model) entryRow(cw int, i int, e models.Entry) string {
	title, tags := diary.ParseTitleTags(e.Body)
	preview := title
	if preview == "" {
		preview = firstLine(e.Body)
	}
	avail := max(cw-3, 4) // ui.Row's accent/indent prefix takes 2 cells, 1 more is the right margin

	var tagPlain, tagStyled string
	for _, t := range tags {
		tagPlain += " #" + t
		if diary.IsKnownCategory(t) {
			tagStyled += " " + categoryStyle.Render("#"+t)
		} else {
			tagStyled += " " + mutedStyle.Render("#"+t)
		}
	}
	ai := ""
	if e.Generated {
		ai = " " + ui.Pill("AI", ui.OK)
	}
	maxP := avail - dateCol - lipgloss.Width(tagPlain) - lipgloss.Width(ai)
	if maxP < 12 && len(tags) > 0 {
		tagPlain, tagStyled = "", ""
		maxP = avail - dateCol - lipgloss.Width(ai)
	}
	if maxP < 4 && ai != "" { // no room for a readable title: the tag gives way too
		ai = ""
		maxP = avail - dateCol
	}
	preview = ansi.Truncate(preview, max(maxP, 0), "…")
	preview += strings.Repeat(" ", max(maxP-lipgloss.Width(preview), 0))

	var styled string
	switch {
	case m.searchQuery != "":
		styled = highlightMatch(preview, m.searchQuery)
	case title != "":
		styled = titleRowStyle.Render(preview)
	default:
		styled = mutedStyle.Render(preview)
	}
	date := lipgloss.NewStyle().Foreground(theme.SubtleV2).Render(fmt.Sprintf("%-10s", e.Date.Format("2006-01-02")))
	text := date + "  " + styled + tagStyled + ai

	if i == m.hoverRow && i != m.cursor {
		plain := ansi.Strip(text)
		plain = ansi.Truncate(plain, max(cw-2, 0), "…")
		return lipgloss.NewStyle().Background(theme.HoverBgV2).Render("  " + plain + strings.Repeat(" ", max(cw-2-lipgloss.Width(plain), 0)))
	}
	return ui.Row(cw, i == m.cursor, text)
}

// renderEntryList is the entries panel's content for a cw×ch area: the
// palette/search lines, then the visible entry rows.
func (m *Model) renderEntryList(cw, ch int) string {
	lines := m.listHeadLines()
	for i, l := range lines { // palette/search lines must never outgrow a narrow panel
		lines[i] = ansi.Truncate(l, cw, "…")
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
	maxVis, start := m.listWindow(ch)
	for i := start; i < len(entries) && i-start < maxVis; i++ {
		lines = append(lines, m.entryRow(cw, i, entries[i]))
	}
	return strings.Join(lines, "\n")
}

// renderPreview is the right-hand panel (width >= 120): the selected entry's
// date, word count, tags and the start of its body, wrapped to cw.
func (m *Model) renderPreview(cw int) string {
	entries := m.visibleEntries()
	if len(entries) == 0 || m.cursor < 0 || m.cursor >= len(entries) {
		return emptystate.Render(0, 0, "", "Nothing selected", "pick an entry to preview it here")
	}
	e := entries[m.cursor]
	title, tags := diary.ParseTitleTags(e.Body)
	var out []string
	head := lipgloss.NewStyle().Bold(true).Render(e.Date.Format("Mon 02 Jan 2006"))
	// "yesterday", "3d ago", "in 2d" add information; a plain date repeats the line
	if rt := ui.RelTime(e.Date, time.Now()); !strings.ContainsAny(rt, "0123456789") || strings.Contains(rt, "ago") || strings.HasPrefix(rt, "in ") {
		head += mutedStyle.Render("  " + rt)
	}
	out = append(out, head)
	meta := fmt.Sprintf("%d words", diary.WordCount(e.Body))
	if e.Generated {
		meta += " · " + "AI-written"
	}
	out = append(out, mutedStyle.Render(meta))
	if len(tags) > 0 {
		var ts []string
		for _, t := range tags {
			if diary.IsKnownCategory(t) {
				ts = append(ts, categoryStyle.Render("#"+t))
			} else {
				ts = append(ts, mutedStyle.Render("#"+t))
			}
		}
		out = append(out, strings.Join(ts, " "))
	}
	out = append(out, "")
	if title != "" {
		for _, l := range strings.Split(ansi.Wrap(title, cw, ""), "\n") {
			out = append(out, lipgloss.NewStyle().Bold(true).Render(l))
		}
		out = append(out, "")
	}
	skippedTitle := title == "" // the title is already shown bold above; don't repeat its first line
	for _, line := range strings.Split(e.Body, "\n") {
		trim := strings.TrimSpace(line)
		if !skippedTitle && trim != "" {
			skippedTitle = true
			if strings.TrimSpace(strings.TrimLeft(trim, "#")) == title {
				continue
			}
		}
		switch {
		case trim == "":
			out = append(out, "")
		case strings.HasPrefix(trim, "#"):
			h := strings.TrimSpace(strings.TrimLeft(trim, "#"))
			if h == "" || (title != "" && h == title) {
				continue
			}
			for _, l := range strings.Split(ansi.Wrap(h, cw, ""), "\n") {
				out = append(out, lipgloss.NewStyle().Bold(true).Foreground(theme.BlueV2).Render(l))
			}
		default:
			out = append(out, strings.Split(ansi.Wrap(line, cw, ""), "\n")...)
		}
		if len(out) > 80 { // ponytail: the panel cuts to its height anyway; 80 just bounds the work on huge entries
			break
		}
	}
	return strings.Join(out, "\n")
}

// todayLines are the suite activity numbers for the small "Today" panel under
// the heatmap.
func (m *Model) todayLines() string {
	if !m.todayLoaded {
		return mutedStyle.Render("loading…")
	}
	var parts []string
	if m.todayCommits > 0 {
		parts = append(parts, greenStyle.Render(fmt.Sprintf("%d commit%s", m.todayCommits, plural(m.todayCommits))))
	}
	if m.todayTasks > 0 {
		parts = append(parts, fmt.Sprintf("%d task%s done", m.todayTasks, plural(m.todayTasks)))
	}
	if m.todayEvents > 0 {
		parts = append(parts, fmt.Sprintf("%d event%s", m.todayEvents, plural(m.todayEvents)))
	}
	if m.todayDuration > 0 {
		parts = append(parts, diary.FormatDuration(m.todayDuration)+" tracked")
	}
	if len(parts) == 0 {
		return mutedStyle.Render("nothing yet")
	}
	return strings.Join(parts, "\n")
}

func (m *Model) viewList() string {
	g := m.geom()
	w := g.w

	// ── footer: contextual hints left (last ones drop first), flash message right
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

	// ── header: title left, "<n> entries · streak Nd · today …" middle, date right
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
	header += "\n" + ui.Divider(w, "")
	if g.spacious {
		header += "\n"
	}

	// ── body: [heatmap + today] [entries] [preview]
	var cols []string
	if g.heat {
		// the heatmap's own first line ("last 13 weeks") becomes the panel title
		hl := strings.SplitN(m.renderHeatmap(), "\n", 2)
		hc, heatTitle := hl[len(hl)-1], "Last 13 weeks"
		if len(hl) == 2 {
			t := ansi.Strip(hl[0])
			heatTitle = strings.ToUpper(t[:1]) + t[1:]
		}
		heatH := min(len(strings.Split(hc, "\n"))+2, g.bodyH)
		left := g.panel(heatmapPanelW, heatH, heatTitle, hc, false)
		today := m.todayLines()
		if rest := g.bodyH - heatH; rest >= 4 {
			left += "\n" + g.panel(heatmapPanelW, min(rest, strings.Count(today, "\n")+3), "Today", today, false)
		}
		cols = append(cols, left, " ")
	}
	list := g.panel(g.listW, g.bodyH, "Entries", m.renderEntryList(g.contentW(g.listW), g.contentH(g.bodyH)), true)
	cols = append(cols, list)
	if g.wide {
		prev := g.panel(g.prevW, g.bodyH, "Preview", m.renderPreview(g.contentW(g.prevW)), false)
		cols = append(cols, " ", prev)
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, cols...)
	return ui.Frame(g.h, header, body, footer)
}
