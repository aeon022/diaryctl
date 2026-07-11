package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/aeon022/diaryctl/internal/diary"
	"github.com/aeon022/diaryctl/internal/git"
	"github.com/aeon022/diaryctl/internal/models"
	"github.com/aeon022/diaryctl/internal/notectl"
	"github.com/aeon022/diaryctl/internal/store"
	"github.com/aeon022/diaryctl/internal/suite"
	"github.com/spf13/cobra"
)

var (
	todayOpen bool
	todayJSON bool
)

var todayCmd = &cobra.Command{
	Use:   "today",
	Short: "Generate or show today's diary entry",
	Long: `Generate today's diary entry from git history.
If an entry already exists, it will be printed.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()

		today := time.Now()

		entry, err := s.GetEntry(today)
		if err != nil {
			return fmt.Errorf("checking existing entry: %w", err)
		}

		if entry == nil || entry.Body == "" {
			repos, err := s.ListRepos()
			if err != nil {
				return fmt.Errorf("listing repos: %w", err)
			}
			if len(repos) == 0 {
				fmt.Fprintln(os.Stderr, "No repos registered. Run `diaryctl init [path]` to register a git repo.")
				return nil
			}

			ds, err := git.DayStats(repos, today)
			if err != nil {
				return fmt.Errorf("reading git stats: %w", err)
			}

			byRepo, err := git.CommitsByRepo(repos, today)
			if err != nil {
				return fmt.Errorf("reading commits by repo: %w", err)
			}
			ds.ByRepo = byRepo

			streak, _ := s.GetStreak()
			ds.Streak = streak

			tasks, _ := suite.TodayTasks()
			events, _ := suite.TodayEvents()
			times, _ := suite.TodayTimeEntries()
			habits, _ := suite.TodayHabits()

			body := diary.BuildEntryBody(ds, tasks, events, times, habits)

			if err := s.SaveEntry(today, body, false); err != nil {
				return fmt.Errorf("saving entry: %w", err)
			}
			_ = notectl.WriteBack(today, body)

			entry, err = s.GetEntry(today)
			if err != nil {
				return fmt.Errorf("reading saved entry: %w", err)
			}
		}

		if todayJSON {
			type out struct {
				Date      string `json:"date"`
				Body      string `json:"body"`
				Generated bool   `json:"generated"`
			}
			b, _ := json.MarshalIndent(out{
				Date:      entry.Date.Format("2006-01-02"),
				Body:      entry.Body,
				Generated: entry.Generated,
			}, "", "  ")
			fmt.Println(string(b))
			return nil
		}

		fmt.Println(entry.Body)

		if todayOpen {
			if err := openEntryInEditor(s, entry); err != nil {
				return err
			}
		}

		return nil
	},
}

func init() {
	todayCmd.Flags().BoolVar(&todayOpen, "open", false, "Open the entry in $EDITOR after generating")
	todayCmd.Flags().BoolVar(&todayJSON, "json", false, "Output as JSON")
}

// openEntryInEditor opens an entry in $EDITOR and saves the result.
func openEntryInEditor(s *store.Store, entry *models.Entry) error {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	tmpFile, err := os.CreateTemp("", "diaryctl-*.md")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpFile.WriteString(entry.Body)
	tmpFile.Close()

	c := exec.Command(editor, tmpFile.Name())
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("opening editor: %w", err)
	}

	content, err := os.ReadFile(tmpFile.Name())
	os.Remove(tmpFile.Name())
	if err != nil {
		return fmt.Errorf("reading edited file: %w", err)
	}
	if err := s.SaveEntry(entry.Date, string(content), entry.Generated); err != nil {
		return fmt.Errorf("saving edited entry: %w", err)
	}
	_ = notectl.WriteBack(entry.Date, string(content))
	fmt.Println("Entry saved.")
	return nil
}
