package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/aeon022/diaryctl/internal/store"
)

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

// motionThrottleFilter drops MouseMotionMsg messages arriving <16ms apart —
// all-motion mouse mode otherwise re-renders on every pixel of movement.
func motionThrottleFilter() func(tea.Model, tea.Msg) tea.Msg {
	var lastMotion time.Time
	return func(_ tea.Model, msg tea.Msg) tea.Msg {
		if _, ok := msg.(tea.MouseMotionMsg); !ok {
			return msg
		}
		now := time.Now()
		if now.Sub(lastMotion) < 16*time.Millisecond {
			return nil
		}
		lastMotion = now
		return msg
	}
}

// ── Run ───────────────────────────────────────────────────────────────────────

func Run(s *store.Store) error {
	m := New(s)
	p := tea.NewProgram(m, tea.WithFilter(motionThrottleFilter()), tea.WithFPS(30))
	_, err := p.Run()
	return err
}
