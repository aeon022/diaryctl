package suite

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExpandPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for in, want := range map[string]string{
		"~/notes":   filepath.Join(home, "notes"),
		"~":         home,
		"/abs/path": "/abs/path",
		"rel/path":  "rel/path",
		"a/~/b":     "a/~/b", // only a LEADING tilde expands
		"":          "",
	} {
		if got, err := ExpandPath(in); err != nil || got != want {
			t.Errorf("ExpandPath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestTotalDuration(t *testing.T) {
	if TotalDuration(nil) != 0 {
		t.Error("empty must be 0")
	}
	got := TotalDuration([]TimeEntry{{Duration: 90 * time.Minute}, {Duration: 30 * time.Minute}})
	if got != 2*time.Hour {
		t.Errorf("TotalDuration = %v, want 2h", got)
	}
}

func TestTodayTasksReadsOnlyTodaysCompleted(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// not installed → empty, no error (diary must still build without taskctl)
	if tasks, err := TodayTasks(); err != nil || len(tasks) != 0 {
		t.Fatalf("no taskctl db: %v, %v", tasks, err)
	}

	dir := filepath.Join(home, "Library", "Application Support", "taskctl")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "taskctl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE tasks (title TEXT, list TEXT, status TEXT, completed_at TEXT)`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	rows := [][]any{
		{"done today", "Work", "completed", now.Format(time.RFC3339)},
		{"done long ago", "Work", "completed", now.AddDate(0, 0, -3).Format(time.RFC3339)},
		{"still open", "Work", "needsAction", now.Format(time.RFC3339)},
	}
	for _, r := range rows {
		if _, err := db.Exec(`INSERT INTO tasks VALUES (?,?,?,?)`, r...); err != nil {
			t.Fatal(err)
		}
	}

	tasks, err := TodayTasks()
	if err != nil || len(tasks) != 1 || tasks[0].Title != "done today" || tasks[0].List != "Work" {
		t.Errorf("TodayTasks = %+v, %v; want only 'done today'", tasks, err)
	}
	if tasks[0].CompletedAt.IsZero() {
		t.Error("CompletedAt not parsed")
	}
}
