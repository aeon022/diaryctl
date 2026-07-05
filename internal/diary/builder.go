// Package diary provides entry building logic shared between CLI and TUI.
package diary

import (
	"fmt"
	"strings"
	"time"

	"github.com/aeon022/diaryctl/internal/git"
	"github.com/aeon022/diaryctl/internal/models"
	"github.com/aeon022/diaryctl/internal/store"
)

// BuildEntryBody constructs the markdown diary entry body for the given date.
func BuildEntryBody(repos []models.Repo, date time.Time, s *store.Store) (string, error) {
	ds, err := git.DayStats(repos, date)
	if err != nil {
		return "", err
	}
	byRepo, err := git.CommitsByRepo(repos, date)
	if err != nil {
		return "", err
	}
	streak, _ := s.GetStreak()

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s\n\n", date.Format("2006-01-02")))
	sb.WriteString("## Stats\n")
	sb.WriteString(fmt.Sprintf("- **Commits:** %d across %d repos\n", len(ds.Commits), len(ds.Repos)))
	sb.WriteString(fmt.Sprintf("- **Files changed:** %d\n", ds.TotalFiles))
	sb.WriteString(fmt.Sprintf("- **Lines:** +%d / -%d\n", ds.TotalAdded, ds.TotalDeleted))

	if len(ds.Commits) > 0 {
		earliest := ds.Commits[0].Timestamp
		latest := ds.Commits[0].Timestamp
		for _, c := range ds.Commits {
			if c.Timestamp.Before(earliest) {
				earliest = c.Timestamp
			}
			if c.Timestamp.After(latest) {
				latest = c.Timestamp
			}
		}
		sb.WriteString(fmt.Sprintf("- **Active window:** %s – %s (estimated)\n",
			earliest.Format("15:04"), latest.Format("15:04")))
	}
	sb.WriteString(fmt.Sprintf("- **Streak:** %d days\n", streak))
	sb.WriteString("\n## Commits\n")

	for repoName, commits := range byRepo {
		sb.WriteString(fmt.Sprintf("### %s\n", repoName))
		for _, c := range commits {
			sb.WriteString(fmt.Sprintf("- `%s` %s\n", c.Hash, c.Message))
		}
		sb.WriteString("\n")
	}

	if len(byRepo) == 0 {
		sb.WriteString("_No commits today._\n\n")
	}

	sb.WriteString("## Context\n")
	sb.WriteString("<!-- AI: Read the commits above and write a narrative diary entry. What was the main focus?\n")
	sb.WriteString("     What was hard? What felt good? Keep it personal, like a real developer diary.\n")
	sb.WriteString("     2-3 paragraphs max. -->\n\n")
	sb.WriteString("## Reflection\n")
	sb.WriteString("<!-- What did you learn today? What would you do differently? -->\n\n")
	sb.WriteString("## Tomorrow\n")
	sb.WriteString("<!-- What's the plan? -->\n")

	return sb.String(), nil
}
