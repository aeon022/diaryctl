// Package diary provides entry building logic shared between CLI and TUI.
package diary

import (
	"fmt"
	"strings"
	"time"

	"github.com/aeon022/diaryctl/internal/models"
	"github.com/aeon022/diaryctl/internal/suite"
)

// formatDuration formats a duration as "Xh Ym" (no seconds).
func formatDuration(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 && m > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	if h > 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dm", m)
}

// BuildEntryBody constructs the markdown diary entry body for the given day.
func BuildEntryBody(
	stats models.DayStats,
	tasks []suite.CompletedTask,
	events []suite.CalendarEvent,
	timeEntries []suite.TimeEntry,
	habits []suite.HabitStatus,
) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# %s\n\n", stats.Date.Format("2006-01-02")))

	// --- Stats ---
	sb.WriteString("## Stats\n")
	sb.WriteString(fmt.Sprintf("- **Commits:** %d across %d repos\n", len(stats.Commits), len(stats.Repos)))
	sb.WriteString(fmt.Sprintf("- **Files changed:** %d  |  **Lines:** +%d / -%d\n", stats.TotalFiles, stats.TotalAdded, stats.TotalDeleted))
	sb.WriteString(fmt.Sprintf("- **Streak:** %d days\n", stats.Streak))
	if len(timeEntries) > 0 {
		total := suite.TotalDuration(timeEntries)
		sb.WriteString(fmt.Sprintf("- **Time tracked:** %s\n", formatDuration(total)))
	}
	sb.WriteString("\n")

	// --- Calendar ---
	if len(events) > 0 {
		sb.WriteString("## Calendar\n")
		for _, e := range events {
			line := fmt.Sprintf("- %s–%s  %s (%s)",
				e.Start.Format("15:04"), e.End.Format("15:04"), e.Title, e.Calendar)
			if e.Location != "" {
				line += fmt.Sprintf(" @ %s", e.Location)
			}
			sb.WriteString(line + "\n")
		}
		sb.WriteString("\n")
	}

	// --- Completed Tasks ---
	if len(tasks) > 0 {
		sb.WriteString("## Completed Tasks\n")
		for _, t := range tasks {
			sb.WriteString(fmt.Sprintf("- [%s] %s\n", t.List, t.Title))
		}
		sb.WriteString("\n")
	}

	// --- Time Log ---
	if len(timeEntries) > 0 {
		sb.WriteString("## Time Log\n")
		for _, e := range timeEntries {
			line := fmt.Sprintf("- %s  %s", formatDuration(e.Duration), e.Task)
			if e.Project != "" {
				line += fmt.Sprintf("  [%s]", e.Project)
			}
			sb.WriteString(line + "\n")
		}
		sb.WriteString("\n")
	}

	// --- Habits ---
	if len(habits) > 0 {
		sb.WriteString("## Habits\n")
		for _, h := range habits {
			if h.CheckedToday {
				streakStr := ""
				if h.Streak > 1 {
					streakStr = fmt.Sprintf(" (streak: %d)", h.Streak)
				}
				sb.WriteString(fmt.Sprintf("- [x] %s%s\n", h.Name, streakStr))
			} else {
				sb.WriteString(fmt.Sprintf("- [ ] %s\n", h.Name))
			}
		}
		sb.WriteString("\n")
	}

	// --- Commits ---
	sb.WriteString("## Commits\n")
	if len(stats.ByRepo) > 0 {
		for repoName, commits := range stats.ByRepo {
			sb.WriteString(fmt.Sprintf("### %s\n", repoName))
			for _, c := range commits {
				sb.WriteString(fmt.Sprintf("- `%s` %s\n", c.Hash, c.Message))
			}
			sb.WriteString("\n")
		}
	} else {
		sb.WriteString("_No commits today._\n\n")
	}

	sb.WriteString("## Context\n")
	sb.WriteString("<!-- AI: Read everything above and write a narrative diary entry.\n")
	sb.WriteString("     What was the main focus? What was hard? What felt good? What did you learn?\n")
	sb.WriteString("     Keep it personal, like a real developer diary. 2-3 paragraphs. -->\n\n")
	sb.WriteString("## Reflection\n")
	sb.WriteString("<!-- What did you learn today? What would you do differently? -->\n\n")
	sb.WriteString("## Tomorrow\n")
	sb.WriteString("<!-- What's the plan? -->\n")

	return sb.String()
}
