package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "diary.db"), false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func TestResolveDBPath(t *testing.T) {
	t.Setenv("DIARYCTL_DATA_DIR", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	p, shared, err := ResolveDBPath()
	if err != nil || shared || p != filepath.Join(home, ".local", "share", "diaryctl", "diary.db") {
		t.Errorf("default = %q shared=%v err=%v", p, shared, err)
	}

	dir := filepath.Join(t.TempDir(), "sync")
	t.Setenv("DIARYCTL_DATA_DIR", dir)
	p, shared, err = ResolveDBPath()
	if err != nil || !shared || p != filepath.Join(dir, "diary.db") {
		t.Errorf("override = %q shared=%v err=%v", p, shared, err)
	}
}

func TestEntryRoundTripAndUpsert(t *testing.T) {
	s := openTemp(t)
	d := day(2026, 10, 5)

	if e, err := s.GetEntry(d); err != nil || e != nil {
		t.Fatalf("missing entry = %+v, %v; want nil, nil", e, err)
	}
	if err := s.SaveEntry(d, "first", true); err != nil {
		t.Fatal(err)
	}
	e, err := s.GetEntry(d)
	if err != nil || e == nil || e.Body != "first" || !e.Generated || !e.Date.Equal(d) {
		t.Fatalf("GetEntry = %+v, %v", e, err)
	}
	id := e.ID

	// same date again = update in place (same row), flags follow the new write
	if err := s.SaveEntry(d, "second", false); err != nil {
		t.Fatal(err)
	}
	e, _ = s.GetEntry(d)
	if e.ID != id || e.Body != "second" || e.Generated {
		t.Errorf("after upsert: %+v (want id %d, body second, generated=false)", e, id)
	}
	if es, _ := s.ListEntries(10); len(es) != 1 {
		t.Errorf("upsert created %d rows, want 1", len(es))
	}

	if err := s.DeleteEntry(d); err != nil {
		t.Fatal(err)
	}
	if e, _ := s.GetEntry(d); e != nil {
		t.Error("entry still present after DeleteEntry")
	}
}

func TestListEntriesOrderAndLimit(t *testing.T) {
	s := openTemp(t)
	for _, d := range []int{3, 1, 5, 2, 4} {
		if err := s.SaveEntry(day(2026, 10, d), "x", false); err != nil {
			t.Fatal(err)
		}
	}
	es, err := s.ListEntries(3)
	if err != nil || len(es) != 3 {
		t.Fatalf("ListEntries(3) = %d, %v", len(es), err)
	}
	for i, want := range []int{5, 4, 3} { // newest first
		if es[i].Date.Day() != want {
			t.Errorf("entry %d = day %d, want %d", i, es[i].Date.Day(), want)
		}
	}
}

func TestRepos(t *testing.T) {
	s := openTemp(t)
	_ = s.SaveRepo("/r/b", "beta")
	_ = s.SaveRepo("/r/a", "alpha")
	_ = s.SaveRepo("/r/b", "beta-renamed") // same path: rename, not duplicate

	rs, err := s.ListRepos()
	if err != nil || len(rs) != 2 {
		t.Fatalf("ListRepos = %+v, %v", rs, err)
	}
	if rs[0].Name != "alpha" || rs[1].Name != "beta-renamed" { // ordered by name
		t.Errorf("repos = %+v", rs)
	}
	_ = s.DeleteRepo("/r/a")
	if rs, _ = s.ListRepos(); len(rs) != 1 || rs[0].Path != "/r/b" {
		t.Errorf("after delete: %+v", rs)
	}
}

func TestOpenSamePathTwiceInProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diary.db")
	a, err := Open(path, false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Open(path, false) // diaryctl opens a fresh Store per operation
	if err != nil {
		t.Fatalf("second Open of the same path in one process failed: %v", err)
	}
	_ = a.SaveEntry(day(2026, 1, 1), "x", false)
	if e, _ := b.GetEntry(day(2026, 1, 1)); e == nil {
		t.Error("second handle doesn't see the first handle's write")
	}
	a.Close()
	b.Close()
	c, err := Open(path, false) // lock fully released after both Closes
	if err != nil {
		t.Fatalf("Open after Close: %v", err)
	}
	c.Close()
}

// localToday is "today" as the user sees it, the way entries are keyed.
func localToday() time.Time {
	n := time.Now()
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}

func saveDaysAgo(t *testing.T, s *Store, ago ...int) {
	t.Helper()
	for _, a := range ago {
		if err := s.SaveEntry(localToday().AddDate(0, 0, -a), "work", false); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStreak(t *testing.T) {
	cases := []struct {
		name string
		ago  []int
		want int
	}{
		{"empty", nil, 0},
		{"today only", []int{0}, 1},
		{"yesterday only still counts (today not written yet)", []int{1}, 1},
		{"three in a row from today", []int{0, 1, 2}, 3},
		{"three in a row from yesterday", []int{1, 2, 3}, 3},
		{"two days ago is already broken", []int{2, 3}, 0},
		{"gap in the middle ends the streak", []int{0, 1, 3, 4}, 2},
		{"one-day gap right after the head must not be bridged", []int{0, 2}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := openTemp(t)
			saveDaysAgo(t, s, c.ago...)
			if got, err := s.GetStreak(); err != nil || got != c.want {
				t.Errorf("GetStreak = %d, %v; want %d", got, err, c.want)
			}
		})
	}
}

func TestStreakIgnoresEmptyBodies(t *testing.T) {
	s := openTemp(t)
	_ = s.SaveEntry(localToday(), "", false)
	_ = s.SaveEntry(localToday().AddDate(0, 0, -1), "real", false)
	if got, _ := s.GetStreak(); got != 1 {
		t.Errorf("GetStreak = %d, want 1 (empty-body day doesn't count but yesterday does)", got)
	}
}

// Regression: "today" used to be time.Now().Truncate(24h), i.e. the UTC day.
// Whenever the local calendar day differs from the UTC day (every night east
// of UTC, every evening west of it) an entry for the local today matched
// neither "today" nor "yesterday" and the streak read 0.
func TestStreakUsesLocalCalendarDay(t *testing.T) {
	old := time.Local
	defer func() { time.Local = old }()
	// pick an offset that guarantees local date != UTC date right now
	off := -12 * 3600
	if time.Now().UTC().Hour() >= 10 {
		off = 14 * 3600
	}
	time.Local = time.FixedZone("test", off)
	if time.Now().Format("2006-01-02") == time.Now().UTC().Format("2006-01-02") {
		t.Skip("could not construct a zone whose date differs from UTC")
	}

	s := openTemp(t)
	_ = s.SaveEntry(time.Now(), "work", false) // keyed by the local date
	if got, _ := s.GetStreak(); got != 1 {
		t.Errorf("GetStreak = %d, want 1 for an entry written today (local %s, UTC %s)",
			got, time.Now().Format("2006-01-02"), time.Now().UTC().Format("2006-01-02"))
	}
}

func TestOpenRejectsICloudPlaceholder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "diary.db")
	if err := writeFile(filepath.Join(dir, ".diary.db.icloud")); err != nil {
		t.Fatal(err)
	}
	_, err := Open(path, true)
	if err == nil || !strings.Contains(err.Error(), "iCloud") {
		t.Errorf("Open with iCloud placeholder = %v, want an iCloud download hint", err)
	}
}

func writeFile(p string) error { return os.WriteFile(p, nil, 0o644) }
