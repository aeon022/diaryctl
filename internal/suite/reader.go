// Package suite reads data from other missionctl suite apps (taskctl, calctl, timectl).
// All reads are best-effort: if a database doesn't exist the function returns an empty
// slice and no error so that diaryctl works fine when the other tools aren't installed.
package suite

import (
	"database/sql"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// CompletedTask is a task that was completed today in taskctl.
type CompletedTask struct {
	Title       string
	List        string
	CompletedAt time.Time
}

// CalendarEvent is a calendar event from calctl.
type CalendarEvent struct {
	Title    string
	Start    time.Time
	End      time.Time
	Calendar string
	Location string
}

// TimeEntry is a completed time-tracking entry from timectl.
type TimeEntry struct {
	Task      string
	Project   string
	StartedAt time.Time
	StoppedAt time.Time
	Duration  time.Duration
}

// TodayTasks returns tasks completed today from taskctl's database.
// Returns an empty slice (no error) if taskctl is not installed.
func TodayTasks() ([]CompletedTask, error) {
	path, err := expandPath("~/Library/Application Support/taskctl/taskctl.db")
	if err != nil {
		return nil, nil
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}

	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return nil, nil
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT title, list, completed_at
		FROM tasks
		WHERE status = 'completed'
		  AND date(completed_at) = date('now')
		ORDER BY completed_at
	`)
	if err != nil {
		return nil, nil
	}
	defer rows.Close()

	var tasks []CompletedTask
	for rows.Next() {
		var t CompletedTask
		var completedAt string
		if err := rows.Scan(&t.Title, &t.List, &completedAt); err != nil {
			continue
		}
		t.CompletedAt, _ = time.Parse(time.RFC3339, completedAt)
		tasks = append(tasks, t)
	}
	return tasks, nil
}

// TodayEvents returns calendar events for today from calctl's database.
// Returns an empty slice (no error) if calctl is not installed.
func TodayEvents() ([]CalendarEvent, error) {
	path, err := expandPath("~/Library/Application Support/calctl/calctl.db")
	if err != nil {
		return nil, nil
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}

	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return nil, nil
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT title, start_time, end_time, calendar, location
		FROM events
		WHERE date(start_time) = date('now')
		ORDER BY start_time
	`)
	if err != nil {
		return nil, nil
	}
	defer rows.Close()

	var events []CalendarEvent
	for rows.Next() {
		var e CalendarEvent
		var startTime, endTime string
		if err := rows.Scan(&e.Title, &startTime, &endTime, &e.Calendar, &e.Location); err != nil {
			continue
		}
		e.Start, _ = time.Parse(time.RFC3339, startTime)
		e.End, _ = time.Parse(time.RFC3339, endTime)
		events = append(events, e)
	}
	return events, nil
}

// TodayTimeEntries returns completed time entries for today from timectl's database.
// Returns an empty slice (no error) if timectl is not installed.
func TodayTimeEntries() ([]TimeEntry, error) {
	path, err := expandPath("~/.local/share/timectl/time.db")
	if err != nil {
		return nil, nil
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}

	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return nil, nil
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT task, project, started_at, stopped_at
		FROM entries
		WHERE date(started_at) = date('now')
		  AND stopped_at IS NOT NULL
		ORDER BY started_at
	`)
	if err != nil {
		return nil, nil
	}
	defer rows.Close()

	var entries []TimeEntry
	for rows.Next() {
		var e TimeEntry
		var startedAt, stoppedAt string
		if err := rows.Scan(&e.Task, &e.Project, &startedAt, &stoppedAt); err != nil {
			continue
		}
		e.StartedAt, _ = time.Parse(time.RFC3339, startedAt)
		e.StoppedAt, _ = time.Parse(time.RFC3339, stoppedAt)
		e.Duration = e.StoppedAt.Sub(e.StartedAt)
		entries = append(entries, e)
	}
	return entries, nil
}

// TotalDuration sums durations across time entries.
func TotalDuration(entries []TimeEntry) time.Duration {
	var total time.Duration
	for _, e := range entries {
		total += e.Duration
	}
	return total
}

// expandPath replaces a leading ~ with the user's home directory.
func expandPath(path string) (string, error) {
	if len(path) > 0 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, path[1:]), nil
	}
	return path, nil
}
