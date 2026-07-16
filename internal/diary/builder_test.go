package diary

import (
	"strings"
	"testing"
	"time"

	"github.com/aeon022/diaryctl/internal/models"
	"github.com/aeon022/diaryctl/internal/suite"
)

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{90 * time.Minute, "1h 30m"},
		{2 * time.Hour, "2h"},
		{45 * time.Minute, "45m"},
		{0, "0m"},
	}
	for _, c := range cases {
		if got := formatDuration(c.in); got != c.want {
			t.Errorf("formatDuration(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBuildEntryBodyFull(t *testing.T) {
	date := time.Date(2026, 7, 15, 0, 0, 0, 0, time.Local)
	stats := models.DayStats{
		Date:         date,
		Repos:        []string{"myproject"},
		Commits:      []models.CommitStat{{Hash: "abc1234", Message: "feat: add login"}},
		ByRepo:       map[string][]models.CommitStat{"myproject": {{Hash: "abc1234", Message: "feat: add login"}}},
		TotalFiles:   3,
		TotalAdded:   120,
		TotalDeleted: 15,
		Streak:       4,
	}
	tasks := []suite.CompletedTask{{Title: "Review PR", List: "Work"}}
	events := []suite.CalendarEvent{{
		Title: "Standup", Calendar: "Work",
		Start: date.Add(9 * time.Hour), End: date.Add(9*time.Hour + 30*time.Minute),
	}}
	timeEntries := []suite.TimeEntry{{Task: "Deep work", Project: "myproject", Duration: 2 * time.Hour}}
	habits := []suite.HabitStatus{
		{Name: "Meditate", CheckedToday: true, Streak: 7},
		{Name: "Read", CheckedToday: false},
	}

	body := BuildEntryBody(stats, tasks, events, timeEntries, habits)

	for _, want := range []string{
		"# 2026-07-15",
		"## Stats",
		"**Commits:** 1 across 1 repos",
		"+120 / -15",
		"**Streak:** 4 days",
		"**Time tracked:** 2h",
		"## Calendar",
		"09:00–09:30  Standup (Work)",
		"## Completed Tasks",
		"- [Work] Review PR",
		"## Time Log",
		"- 2h  Deep work  [myproject]",
		"## Habits",
		"- [x] Meditate (streak: 7)",
		"- [ ] Read",
		"### myproject",
		"- `abc1234` feat: add login",
		"## Context",
		"## Reflection",
		"## Tomorrow",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("entry body missing %q\n---\n%s", want, body)
		}
	}
}

func TestBuildEntryBodyEmptyDay(t *testing.T) {
	stats := models.DayStats{Date: time.Date(2026, 7, 15, 0, 0, 0, 0, time.Local)}
	body := BuildEntryBody(stats, nil, nil, nil, nil)

	if !strings.Contains(body, "_No commits today._") {
		t.Error("empty day must state that there are no commits")
	}
	// optional sections must be omitted entirely
	for _, absent := range []string{"## Calendar", "## Completed Tasks", "## Time Log", "## Habits"} {
		if strings.Contains(body, absent) {
			t.Errorf("empty day must not contain %q", absent)
		}
	}
}
