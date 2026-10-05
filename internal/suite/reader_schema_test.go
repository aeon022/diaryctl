package suite

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Exact DDL of the sibling tools' current schemas (copied from their store
// migrations) so these tests exercise the reader against the real column set,
// NOT NULL constraints and timestamp formats — not a hand-simplified table.
const (
	taskctlDDL = `CREATE TABLE tasks (
		id TEXT PRIMARY KEY, title TEXT NOT NULL, list TEXT NOT NULL DEFAULT '',
		notes TEXT NOT NULL DEFAULT '', url TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'needsAction', due_date TEXT,
		priority INTEGER NOT NULL DEFAULT 0, recurrence TEXT NOT NULL DEFAULT '',
		subtasks TEXT NOT NULL DEFAULT '[]', external_id TEXT NOT NULL DEFAULT '',
		source TEXT NOT NULL DEFAULT 'apple', created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL, completed_at TEXT)`
	calctlDDL = `CREATE TABLE events (
		id TEXT PRIMARY KEY, title TEXT NOT NULL, start_time TEXT NOT NULL,
		end_time TEXT NOT NULL, all_day INTEGER NOT NULL DEFAULT 0,
		calendar TEXT NOT NULL DEFAULT '', location TEXT NOT NULL DEFAULT '',
		notes TEXT NOT NULL DEFAULT '', attendees TEXT NOT NULL DEFAULT '[]',
		source TEXT NOT NULL DEFAULT 'apple', external_id TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`
	timectlDDL = `CREATE TABLE entries (
		id INTEGER PRIMARY KEY AUTOINCREMENT, task TEXT NOT NULL,
		project TEXT NOT NULL DEFAULT '', started_at TEXT NOT NULL, stopped_at TEXT,
		notes TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT (datetime('now')),
		linked_task TEXT NOT NULL DEFAULT '', linked_task_id TEXT NOT NULL DEFAULT '')`
	habctlDDL = `CREATE TABLE habits (
		id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE,
		description TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL,
		group_id INTEGER, freq_target INTEGER NOT NULL DEFAULT 0,
		skip_allowed INTEGER NOT NULL DEFAULT 0, archived INTEGER NOT NULL DEFAULT 0);
	CREATE TABLE checkins (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		habit_id INTEGER NOT NULL REFERENCES habits(id) ON DELETE CASCADE,
		date TEXT NOT NULL, created_at TEXT NOT NULL, note TEXT NOT NULL DEFAULT '',
		UNIQUE(habit_id, date))`
)

// sandbox gives the test an empty HOME and no data-dir overrides, so nothing
// can resolve to the developer's real databases.
func sandbox(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, k := range []string{"TASKCTL", "CALCTL", "TIMECTL", "HABCTL"} {
		t.Setenv(k+"_DATA_DIR", "")
	}
	return home
}

func mkdb(t *testing.T, path, ddl string) *sql.DB {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(ddl); err != nil {
		t.Fatal(err)
	}
	return db
}

func exec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

// localAt is today's date at h:m local time — the boundary cases sit right
// after local midnight, where the UTC date can still be "yesterday".
func localAt(h, m int) time.Time {
	n := time.Now()
	return time.Date(n.Year(), n.Month(), n.Day(), h, m, 0, 0, time.Local)
}

func utc(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func TestTodayTasksLocalDayBoundaryAndNulls(t *testing.T) {
	home := sandbox(t)
	db := mkdb(t, filepath.Join(home, "Library/Application Support/taskctl/taskctl.db"), taskctlDDL)
	ins := func(id, title, status string, completed any) {
		exec(t, db, `INSERT INTO tasks (id,title,list,status,created_at,updated_at,completed_at) VALUES (?,?,?,?,?,?,?)`,
			id, title, "Work", status, utc(time.Now()), utc(time.Now()), completed)
	}
	ins("1", "just after midnight", "completed", utc(localAt(0, 30)))
	ins("2", "late evening", "completed", utc(localAt(23, 30)))
	ins("3", "yesterday late", "completed", utc(localAt(0, 0).Add(-30*time.Minute)))
	ins("4", "tomorrow early", "completed", utc(localAt(23, 59).Add(31*time.Minute)))
	ins("5", "completed but no timestamp", "completed", nil)
	ins("6", "open", "needsAction", nil)

	got, err := TodayTasks()
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, g := range got {
		titles = append(titles, g.Title)
	}
	want := []string{"just after midnight", "late evening"} // ordered by completion time
	if len(titles) != len(want) || titles[0] != want[0] || titles[1] != want[1] {
		t.Errorf("TodayTasks titles = %v, want %v", titles, want)
	}
	if got[0].List != "Work" || !got[0].CompletedAt.Equal(localAt(0, 30)) {
		t.Errorf("first task fields: %+v", got[0])
	}
}

func TestTodayEvents(t *testing.T) {
	home := sandbox(t)
	db := mkdb(t, filepath.Join(home, "Library/Application Support/calctl/calctl.db"), calctlDDL)
	ins := func(id, title string, start, end time.Time, loc string) {
		exec(t, db, `INSERT INTO events (id,title,start_time,end_time,calendar,location,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?)`,
			id, title, utc(start), utc(end), "Arbeit", loc, utc(time.Now()), utc(time.Now()))
	}
	ins("b", "Review", localAt(15, 0), localAt(16, 0), "Raum 2")
	ins("a", "Standup", localAt(0, 15), localAt(0, 45), "") // just after local midnight
	ins("y", "Yesterday", localAt(0, 0).Add(-2*time.Hour), localAt(0, 0).Add(-time.Hour), "")
	ins("t", "Tomorrow", localAt(23, 59).Add(time.Hour), localAt(23, 59).Add(2*time.Hour), "")

	got, err := TodayEvents()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Title != "Standup" || got[1].Title != "Review" {
		t.Fatalf("TodayEvents = %+v, want [Standup Review] in start order", got)
	}
	if got[1].Location != "Raum 2" || got[1].Calendar != "Arbeit" || got[1].End.Sub(got[1].Start) != time.Hour {
		t.Errorf("event fields: %+v", got[1])
	}
}

func TestTodayTimeEntries(t *testing.T) {
	home := sandbox(t)
	db := mkdb(t, filepath.Join(home, ".local/share/timectl/time.db"), timectlDDL)
	// timectl writes RFC3339Nano in local time (offset included)
	ins := func(task string, start time.Time, stop any) {
		exec(t, db, `INSERT INTO entries (task,project,started_at,stopped_at) VALUES (?,?,?,?)`,
			task, "proj", start.Format(time.RFC3339Nano), stop)
	}
	s1, s2 := localAt(0, 10).Add(123456789*time.Nanosecond), localAt(9, 0)
	ins("early", s1, s1.Add(30*time.Minute).Format(time.RFC3339Nano))
	ins("morning", s2, s2.Add(90*time.Minute).Format(time.RFC3339Nano))
	ins("running", localAt(11, 0), nil) // not stopped → not a completed entry
	y := localAt(0, 0).Add(-time.Hour)
	ins("yesterday", y, y.Add(10*time.Minute).Format(time.RFC3339Nano))

	got, err := TodayTimeEntries()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Task != "early" || got[1].Task != "morning" {
		t.Fatalf("TodayTimeEntries = %+v", got)
	}
	if got[0].Duration != 30*time.Minute || got[1].Duration != 90*time.Minute || got[0].Project != "proj" {
		t.Errorf("durations/project: %+v", got)
	}
	if TotalDuration(got) != 2*time.Hour {
		t.Errorf("TotalDuration = %v, want 2h", TotalDuration(got))
	}
}

func TestTodayTimeEntriesHonoursDataDirEnv(t *testing.T) {
	sandbox(t)
	dir := t.TempDir()
	t.Setenv("TIMECTL_DATA_DIR", dir)
	db := mkdb(t, filepath.Join(dir, "time.db"), timectlDDL)
	s := localAt(8, 0)
	exec(t, db, `INSERT INTO entries (task,started_at,stopped_at) VALUES ('shared',?,?)`,
		s.Format(time.RFC3339Nano), s.Add(time.Hour).Format(time.RFC3339Nano))
	if got, _ := TodayTimeEntries(); len(got) != 1 || got[0].Task != "shared" {
		t.Errorf("entries from TIMECTL_DATA_DIR = %+v", got)
	}
}

func day(offset int) string { return time.Now().AddDate(0, 0, offset).Format("2006-01-02") }

func TestTodayHabits(t *testing.T) {
	home := sandbox(t)
	db := mkdb(t, filepath.Join(home, ".local/share/habctl/habits.db"), habctlDDL)
	habit := func(name string, archived int) int64 {
		r, err := db.Exec(`INSERT INTO habits (name,created_at,archived) VALUES (?,?,?)`, name, utc(time.Now()), archived)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := r.LastInsertId()
		return id
	}
	check := func(id int64, offsets ...int) {
		for _, o := range offsets {
			exec(t, db, `INSERT INTO checkins (habit_id,date,created_at) VALUES (?,?,?)`, id, day(o), utc(time.Now()))
		}
	}
	done := habit("Sport", 0)
	check(done, 0, -1, -2) // today + two days back → streak 3
	open := habit("Lesen", 0)
	check(open, -1, -2, -3) // not yet today, but yesterday's streak is alive
	gap := habit("Yoga", 0)
	check(gap, 0, -2) // gap yesterday → streak is just today
	old := habit("Alt", 1)
	check(old, 0)

	got, err := TodayHabits()
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]HabitStatus{}
	for _, h := range got {
		by[h.Name] = h
	}
	if _, ok := by["Alt"]; ok {
		t.Error("archived habit must not appear in the diary")
	}
	if len(got) != 3 {
		t.Fatalf("habits = %+v", got)
	}
	if h := by["Sport"]; !h.CheckedToday || h.Streak != 3 {
		t.Errorf("Sport = %+v, want checked, streak 3", h)
	}
	if h := by["Lesen"]; h.CheckedToday || h.Streak != 3 {
		t.Errorf("Lesen = %+v, want unchecked today but streak 3 (an open today is not a miss)", h)
	}
	if h := by["Yoga"]; !h.CheckedToday || h.Streak != 1 {
		t.Errorf("Yoga = %+v, want checked, streak 1 (gap yesterday)", h)
	}
}

func TestMissingOrForeignDBIsEmptyNotError(t *testing.T) {
	home := sandbox(t)
	// a db file that exists but lacks the tables (other tool version / not a suite db)
	mkdb(t, filepath.Join(home, ".local/share/habctl/habits.db"), `CREATE TABLE unrelated (x)`)
	if h, err := TodayHabits(); err != nil || len(h) != 0 {
		t.Errorf("foreign habctl db: %v, %v", h, err)
	}
	if e, err := TodayEvents(); err != nil || len(e) != 0 {
		t.Errorf("missing calctl db: %v, %v", e, err)
	}
	if e, err := TodayTimeEntries(); err != nil || len(e) != 0 {
		t.Errorf("missing timectl db: %v, %v", e, err)
	}
}

func TestTodayHabitsPreArchivedSchema(t *testing.T) {
	home := sandbox(t)
	db := mkdb(t, filepath.Join(home, ".local/share/habctl/habits.db"), `
		CREATE TABLE habits (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE, description TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL);
		CREATE TABLE checkins (id INTEGER PRIMARY KEY AUTOINCREMENT, habit_id INTEGER NOT NULL, date TEXT NOT NULL, created_at TEXT NOT NULL)`)
	exec(t, db, `INSERT INTO habits (name,created_at) VALUES ('Alt-Schema', ?)`, utc(time.Now()))
	exec(t, db, `INSERT INTO checkins (habit_id,date,created_at) VALUES (1, ?, ?)`, day(0), utc(time.Now()))
	if got, _ := TodayHabits(); len(got) != 1 || got[0].Name != "Alt-Schema" || !got[0].CheckedToday {
		t.Errorf("DB without archived column must still list its habits, got %+v", got)
	}
}
