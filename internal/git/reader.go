package git

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aeon022/diaryctl/internal/models"
)

// shortstatRe parses git --shortstat output lines like:
//
//	3 files changed, 47 insertions(+), 12 deletions(-)
var shortstatRe = regexp.MustCompile(`(\d+) files? changed(?:, (\d+) insertions?\(\+\))?(?:, (\d+) deletions?\(-\))?`)

// DayCommits returns commits made on date across all repos, annotated with diff stats.
func DayCommits(repos []models.Repo, date time.Time) ([]models.CommitStat, error) {
	since := date.Format("2006-01-02") + " 00:00:00"
	until := date.Format("2006-01-02") + " 23:59:59"

	var all []models.CommitStat
	for _, repo := range repos {
		commits, err := repoCommits(repo, since, until)
		if err != nil {
			// Non-fatal: repo may not have commits on this day or git may fail.
			continue
		}
		all = append(all, commits...)
	}
	return all, nil
}

// repoCommits fetches commits for a single repo on a date range.
func repoCommits(repo models.Repo, since, until string) ([]models.CommitStat, error) {
	// First pass: get commit metadata.
	args := []string{
		"-C", repo.Path,
		"log",
		"--since=" + since,
		"--until=" + until,
		"--pretty=format:%H|%s|%an|%ai",
		"--no-merges",
	}
	out, err := runGit(args...)
	if err != nil {
		return nil, err
	}

	lines := splitLines(string(out))
	if len(lines) == 0 {
		return nil, nil
	}

	var commits []models.CommitStat
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) < 4 {
			continue
		}
		ts, err := time.Parse("2006-01-02 15:04:05 -0700", parts[3])
		if err != nil {
			// try alternate format
			ts, _ = time.Parse(time.RFC3339, parts[3])
		}
		commits = append(commits, models.CommitStat{
			Hash:      parts[0][:min(7, len(parts[0]))],
			Message:   parts[1],
			Author:    parts[2],
			Timestamp: ts,
		})
	}

	// Second pass: get numstat for each commit.
	for i := range commits {
		statsArgs := []string{
			"-C", repo.Path,
			"show",
			"--shortstat",
			"--format=",
			commits[i].Hash,
		}
		statsOut, err := runGit(statsArgs...)
		if err != nil {
			continue
		}
		f, a, d := parseShortstat(string(statsOut))
		commits[i].Files = f
		commits[i].Additions = a
		commits[i].Deletions = d
	}

	return commits, nil
}

// DayStats aggregates commit activity for date across all repos.
func DayStats(repos []models.Repo, date time.Time) (models.DayStats, error) {
	stats := models.DayStats{Date: date}

	commits, err := DayCommits(repos, date)
	if err != nil {
		return stats, err
	}
	stats.Commits = commits

	repoSet := map[string]bool{}
	var earliest, latest time.Time
	for _, c := range commits {
		stats.TotalFiles += c.Files
		stats.TotalAdded += c.Additions
		stats.TotalDeleted += c.Deletions
		// track unique repos by author/timestamp — approximate.
		if earliest.IsZero() || c.Timestamp.Before(earliest) {
			earliest = c.Timestamp
		}
		if latest.IsZero() || c.Timestamp.After(latest) {
			latest = c.Timestamp
		}
		_ = repoSet
	}

	// Collect repo names that had commits.
	for _, repo := range repos {
		rc, _ := repoCommits(repo, date.Format("2006-01-02")+" 00:00:00", date.Format("2006-01-02")+" 23:59:59")
		if len(rc) > 0 {
			stats.Repos = append(stats.Repos, repo.Name)
		}
	}

	if !earliest.IsZero() && !latest.IsZero() {
		stats.ActiveMins = int(latest.Sub(earliest).Minutes())
		if stats.ActiveMins == 0 && len(commits) > 0 {
			stats.ActiveMins = 5
		}
	}

	return stats, nil
}

// RecentDays returns DayStats for the last n days (today inclusive).
func RecentDays(repos []models.Repo, n int) ([]models.DayStats, error) {
	today := time.Now()
	result := make([]models.DayStats, 0, n)
	for i := 0; i < n; i++ {
		d := today.AddDate(0, 0, -i)
		ds, err := DayStats(repos, d)
		if err != nil {
			return nil, fmt.Errorf("stats for %s: %w", d.Format("2006-01-02"), err)
		}
		result = append(result, ds)
	}
	return result, nil
}

// CommitsByRepo groups commits by repo name, given parallel repo list.
func CommitsByRepo(repos []models.Repo, date time.Time) (map[string][]models.CommitStat, error) {
	result := make(map[string][]models.CommitStat)
	since := date.Format("2006-01-02") + " 00:00:00"
	until := date.Format("2006-01-02") + " 23:59:59"
	for _, repo := range repos {
		commits, err := repoCommits(repo, since, until)
		if err != nil || len(commits) == 0 {
			continue
		}
		result[repo.Name] = commits
	}
	return result, nil
}

// --- helpers ---

func runGit(args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w\n%s", strings.Join(args[:min(3, len(args))], " "), err, stderr.String())
	}
	return stdout.Bytes(), nil
}

func splitLines(s string) []string {
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}

func parseShortstat(s string) (files, additions, deletions int) {
	m := shortstatRe.FindStringSubmatch(s)
	if m == nil {
		return
	}
	files, _ = strconv.Atoi(m[1])
	if m[2] != "" {
		additions, _ = strconv.Atoi(m[2])
	}
	if m[3] != "" {
		deletions, _ = strconv.Atoi(m[3])
	}
	return
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
