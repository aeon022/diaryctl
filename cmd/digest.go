package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/aeon022/diaryctl/internal/git"
	"github.com/spf13/cobra"
)

var (
	digestDays   int
	digestOutput string
)

var digestCmd = &cobra.Command{
	Use:   "digest",
	Short: "Weekly (or N-day) rollup of commits and diary entries",
	Long: `Aggregate the last N days (7 by default) into one Markdown digest:
commit stats per day plus each day's diary entry, if one exists.`,
	Example: `  diaryctl digest
  diaryctl digest --days 30 -o august.md`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()

		repos, err := s.ListRepos()
		if err != nil {
			return fmt.Errorf("listing repos: %w", err)
		}

		dayStats, err := git.RecentDays(repos, digestDays)
		if err != nil {
			return fmt.Errorf("gathering stats: %w", err)
		}

		entries, err := s.ListEntries(100000)
		if err != nil {
			return fmt.Errorf("listing entries: %w", err)
		}
		entryByDate := make(map[string]string, len(entries))
		for _, e := range entries {
			entryByDate[e.Date.Format("2006-01-02")] = e.Body
		}

		var b strings.Builder
		totalCommits, totalFiles, totalAdded, totalDeleted, activeDays, entryDays := 0, 0, 0, 0, 0, 0
		repoSet := map[string]bool{}

		// RecentDays returns most-recent-first; read chronologically both
		// for the header's date range and the per-day sections below.
		if len(dayStats) > 0 {
			from := dayStats[len(dayStats)-1].Date.Format("Jan 02")
			to := dayStats[0].Date.Format("Jan 02, 2006")
			fmt.Fprintf(&b, "# Digest — %s – %s\n\n", from, to)
		} else {
			b.WriteString("# Digest\n\n")
		}

		var days strings.Builder
		for i := len(dayStats) - 1; i >= 0; i-- {
			ds := dayStats[i]
			if len(ds.Commits) > 0 {
				activeDays++
			}
			totalCommits += len(ds.Commits)
			totalFiles += ds.TotalFiles
			totalAdded += ds.TotalAdded
			totalDeleted += ds.TotalDeleted
			for _, r := range ds.Repos {
				repoSet[r] = true
			}

			dateKey := ds.Date.Format("2006-01-02")
			body, hasEntry := entryByDate[dateKey]
			if hasEntry {
				entryDays++
			}
			if len(ds.Commits) == 0 && !hasEntry {
				continue // quiet day, nothing to report
			}

			fmt.Fprintf(&days, "## %s\n\n", ds.Date.Format("Mon, Jan 02"))
			if len(ds.Commits) > 0 {
				fmt.Fprintf(&days, "**%d commit(s)** · %s · +%d/-%d, %d file(s)\n\n",
					len(ds.Commits), strings.Join(ds.Repos, ", "), ds.TotalAdded, ds.TotalDeleted, ds.TotalFiles)
			}
			if hasEntry && strings.TrimSpace(body) != "" {
				days.WriteString(strings.TrimSpace(body) + "\n\n")
			}
		}

		fmt.Fprintf(&b, "**%d commits** across **%d repo(s)**, **%d active day(s)**  \n", totalCommits, len(repoSet), activeDays)
		fmt.Fprintf(&b, "+%d / -%d lines, %d file(s) changed  \n", totalAdded, totalDeleted, totalFiles)
		fmt.Fprintf(&b, "Diary entries: %d/%d day(s)\n\n", entryDays, digestDays)
		b.WriteString("---\n\n")
		b.WriteString(days.String())

		w := os.Stdout
		if digestOutput != "" {
			f, err := os.Create(digestOutput)
			if err != nil {
				return fmt.Errorf("create output file: %w", err)
			}
			defer f.Close()
			w = f
		}
		fmt.Fprint(w, b.String())
		if digestOutput != "" {
			fmt.Fprintf(os.Stderr, "Wrote digest → %s\n", digestOutput)
		}
		return nil
	},
}

func init() {
	digestCmd.Flags().IntVar(&digestDays, "days", 7, "Number of days to include")
	digestCmd.Flags().StringVarP(&digestOutput, "output", "o", "", "Output file (default: stdout)")
	rootCmd.AddCommand(digestCmd)
}
