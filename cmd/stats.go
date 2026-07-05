package cmd

import (
	"fmt"
	"time"

	"github.com/aeon022/diaryctl/internal/git"
	"github.com/spf13/cobra"
)

var statsDays int

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show productivity stats",
	Long:  `Display coding stats including streaks, commit counts, and repo breakdowns.`,
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
		if len(repos) == 0 {
			fmt.Println("No repos registered. Run `diaryctl init [path]` to register a git repo.")
			return nil
		}

		dayStats, err := git.RecentDays(repos, statsDays)
		if err != nil {
			return fmt.Errorf("gathering stats: %w", err)
		}

		streak, _ := s.GetStreak()

		// Aggregate.
		totalCommits := 0
		totalFiles := 0
		totalAdded := 0
		totalDeleted := 0
		dowCounts := map[string]int{}
		hourCounts := map[int]int{}
		repoCounts := map[string]int{}
		activeDays := 0

		for _, ds := range dayStats {
			if len(ds.Commits) > 0 {
				activeDays++
			}
			totalCommits += len(ds.Commits)
			totalFiles += ds.TotalFiles
			totalAdded += ds.TotalAdded
			totalDeleted += ds.TotalDeleted
			for _, r := range ds.Repos {
				repoCounts[r] += len(ds.Commits)
			}
			for _, c := range ds.Commits {
				dowCounts[c.Timestamp.Weekday().String()]++
				hourCounts[c.Timestamp.Hour()]++
			}
		}

		// Find most active day of week.
		mostActiveDay := "N/A"
		maxDow := 0
		for day, count := range dowCounts {
			if count > maxDow {
				maxDow = count
				mostActiveDay = day
			}
		}

		// Find most active hour.
		mostActiveHour := -1
		maxHour := 0
		for hour, count := range hourCounts {
			if count > maxHour {
				maxHour = count
				mostActiveHour = hour
			}
		}

		fmt.Printf("\n  Productivity Stats — last %d days\n", statsDays)
		fmt.Println("  " + repeatStr("─", 38))
		fmt.Printf("  %-24s %d days\n", "Streak:", streak)
		fmt.Printf("  %-24s %d\n", "Active days:", activeDays)
		fmt.Printf("  %-24s %d\n", "Total commits:", totalCommits)
		fmt.Printf("  %-24s %d\n", "Files changed:", totalFiles)
		fmt.Printf("  %-24s +%d / -%d\n", "Lines:", totalAdded, totalDeleted)
		fmt.Printf("  %-24s %s\n", "Most active day:", mostActiveDay)
		if mostActiveHour >= 0 {
			fmt.Printf("  %-24s %02d:00–%02d:59\n", "Most active hour:", mostActiveHour, mostActiveHour)
		}

		fmt.Println()
		fmt.Println("  Repo Breakdown:")
		fmt.Println("  " + repeatStr("─", 38))
		for _, repo := range repos {
			count := repoCounts[repo.Name]
			bar := barStr(count, totalCommits, 20)
			fmt.Printf("  %-18s [%s] %d\n", repo.Name, bar, count)
		}

		fmt.Println()
		fmt.Println("  Daily Activity (last 14 days):")
		fmt.Println("  " + repeatStr("─", 38))
		today := time.Now()
		for i := 13; i >= 0; i-- {
			d := today.AddDate(0, 0, -i)
			dateStr := d.Format("Mon 01/02")
			var dayCommits int
			for _, ds := range dayStats {
				if ds.Date.Format("2006-01-02") == d.Format("2006-01-02") {
					dayCommits = len(ds.Commits)
					break
				}
			}
			bar := barStr(dayCommits, 10, 20)
			if dayCommits == 0 {
				bar = repeatStr("·", 20)
			}
			fmt.Printf("  %-12s [%s] %d commits\n", dateStr, bar, dayCommits)
		}

		fmt.Println()
		return nil
	},
}

func init() {
	statsCmd.Flags().IntVar(&statsDays, "days", 30, "Number of days to analyze")
}

func repeatStr(s string, n int) string {
	result := ""
	for i := 0; i < n; i++ {
		result += s
	}
	return result
}

func barStr(count, max, width int) string {
	if max == 0 {
		return repeatStr(" ", width)
	}
	filled := (count * width) / max
	if filled > width {
		filled = width
	}
	return repeatStr("█", filled) + repeatStr(" ", width-filled)
}
