package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/aeon022/diaryctl/internal/models"
)

func TestParseShortstat(t *testing.T) {
	cases := []struct {
		in      string
		f, a, d int
	}{
		{" 3 files changed, 47 insertions(+), 12 deletions(-)\n", 3, 47, 12},
		{" 1 file changed, 1 insertion(+)\n", 1, 1, 0},
		{" 2 files changed, 5 deletions(-)\n", 2, 0, 5},
		{" 1 file changed, 1 insertion(+), 1 deletion(-)", 1, 1, 1},
		{"", 0, 0, 0}, // empty commit / merge: no stat line
		{"garbage", 0, 0, 0},
	}
	for _, c := range cases {
		if f, a, d := parseShortstat(c.in); f != c.f || a != c.a || d != c.d {
			t.Errorf("parseShortstat(%q) = %d,%d,%d want %d,%d,%d", c.in, f, a, d, c.f, c.a, c.d)
		}
	}
}

// newRepo makes a throwaway git repo; commit() adds one commit at a fixed time.
func newRepo(t *testing.T) (dir string, commit func(file, content, msg, when string)) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("TZ", "UTC") // git parses --since/--until in local time; keep it deterministic
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	dir = t.TempDir()
	run := func(env []string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run(nil, "init", "-q")
	run(nil, "config", "user.name", "Test Dev")
	run(nil, "config", "user.email", "dev@example.com")
	run(nil, "config", "commit.gpgsign", "false")
	return dir, func(file, content, msg, when string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		run(nil, "add", file)
		run([]string{"GIT_AUTHOR_DATE=" + when, "GIT_COMMITTER_DATE=" + when}, "commit", "-q", "-m", msg)
	}
}

func TestDayCommitsAndStats(t *testing.T) {
	dir, commit := newRepo(t)
	commit("a.txt", "1\n2\n3\n", "first change", "2026-03-10T09:00:00+0000")
	commit("a.txt", "1\n2\n3\n4\n5\n", "second change", "2026-03-10T09:45:00+0000")
	commit("b.txt", "x\n", "other day", "2026-03-11T10:00:00+0000")

	repos := []models.Repo{{Path: dir, Name: "proj"}, {Path: filepath.Join(dir, "missing"), Name: "gone"}}
	day := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)

	commits, err := DayCommits(repos, day)
	if err != nil {
		t.Fatal(err) // a broken repo must be skipped, not fatal
	}
	if len(commits) != 2 {
		t.Fatalf("got %d commits, want 2 (other day excluded, missing repo skipped): %+v", len(commits), commits)
	}
	c := commits[1] // git log is newest-first, so [1] is "first change"
	if c.Message != "first change" || c.Author != "Test Dev" || c.RepoName != "proj" || len(c.Hash) != 7 {
		t.Errorf("commit = %+v", c)
	}
	if c.Files != 1 || c.Additions != 3 || c.Deletions != 0 {
		t.Errorf("first change stat = files %d +%d -%d, want 1 +3 -0", c.Files, c.Additions, c.Deletions)
	}
	if got := commits[0]; got.Additions != 2 {
		t.Errorf("second change additions = %d, want 2", got.Additions)
	}
	if want := time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC); !c.Timestamp.Equal(want) {
		t.Errorf("Timestamp = %v, want %v", c.Timestamp, want)
	}

	ds, err := DayStats(repos, day)
	if err != nil {
		t.Fatal(err)
	}
	if ds.TotalFiles != 2 || ds.TotalAdded != 5 || ds.TotalDeleted != 0 {
		t.Errorf("totals = files %d +%d -%d", ds.TotalFiles, ds.TotalAdded, ds.TotalDeleted)
	}
	if ds.ActiveMins != 45 {
		t.Errorf("ActiveMins = %d, want 45 (first to last commit)", ds.ActiveMins)
	}
	if len(ds.Repos) != 1 || ds.Repos[0] != "proj" {
		t.Errorf("Repos = %v, want only repos with commits", ds.Repos)
	}

	by, _ := CommitsByRepo(repos, day)
	if len(by) != 1 || len(by["proj"]) != 2 {
		t.Errorf("CommitsByRepo = %v", by)
	}
}

func TestDayStatsSingleCommitGetsMinimumActiveTime(t *testing.T) {
	dir, commit := newRepo(t)
	commit("a.txt", "x\n", "only one", "2026-03-10T12:00:00+0000")
	ds, _ := DayStats([]models.Repo{{Path: dir, Name: "p"}}, time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC))
	if ds.ActiveMins != 5 {
		t.Errorf("ActiveMins = %d, want the 5-minute floor for a lone commit", ds.ActiveMins)
	}
	empty, _ := DayStats(nil, time.Now())
	if empty.ActiveMins != 0 || len(empty.Commits) != 0 {
		t.Errorf("no repos: %+v", empty)
	}
}

// Regression: the log format was "%H|%s|%an|%ai" split on "|", so a subject
// containing a pipe (very common: "fix: a | b", "docs: foo|bar") shifted every
// field, put a fragment of the subject into Author and lost the timestamp.
func TestCommitSubjectWithPipe(t *testing.T) {
	dir, commit := newRepo(t)
	commit("a.txt", "x\n", "fix: parse a|b | c", "2026-03-10T09:00:00+0000")
	cs, _ := DayCommits([]models.Repo{{Path: dir, Name: "p"}}, time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC))
	if len(cs) != 1 {
		t.Fatalf("got %d commits", len(cs))
	}
	c := cs[0]
	if c.Message != "fix: parse a|b | c" || c.Author != "Test Dev" || c.Timestamp.IsZero() {
		t.Errorf("commit with pipe in subject parsed wrong: %+v", c)
	}
}

func TestRecentDays(t *testing.T) {
	days, err := RecentDays(nil, 3)
	if err != nil || len(days) != 3 {
		t.Fatalf("RecentDays = %d, %v", len(days), err)
	}
	if !days[0].Date.After(days[1].Date) { // newest first
		t.Error("RecentDays not ordered newest-first")
	}
}
