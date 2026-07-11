package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var (
	exportDate   string
	exportFormat string
)

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export a diary entry to stdout",
	Long: `Export a diary entry to stdout.

Use --date to pick a specific date (defaults to today).
Use --format raw (default) to emit the body as-is, or --format post to wrap
it in postctl-compatible Markdown frontmatter.

Examples:
  diaryctl export
  diaryctl export --date 2026-07-10
  diaryctl export --date 2026-07-10 --format post | postctl import -
  diaryctl export --date 2026-07-10 > entry.md`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()

		date := time.Now()
		if exportDate != "" {
			date, err = time.Parse("2006-01-02", exportDate)
			if err != nil {
				return fmt.Errorf("invalid date format, use YYYY-MM-DD: %w", err)
			}
		}

		entry, err := s.GetEntry(date)
		if err != nil {
			return fmt.Errorf("reading entry: %w", err)
		}

		if entry == nil || entry.Body == "" {
			fmt.Fprintf(os.Stderr, "no entry found for %s\n", date.Format("2006-01-02"))
			os.Exit(1)
		}

		switch exportFormat {
		case "post":
			fmt.Printf("---\ntitle: \"Diary %s\"\nplatform: twitter\nstatus: draft\ntags: [diary, development]\n---\n%s",
				date.Format("2006-01-02"), entry.Body)
		default: // "raw"
			fmt.Print(entry.Body)
		}

		return nil
	},
}

func init() {
	exportCmd.Flags().StringVar(&exportDate, "date", "", "Date to export (YYYY-MM-DD, default: today)")
	exportCmd.Flags().StringVar(&exportFormat, "format", "raw", "Output format: raw|post")
	rootCmd.AddCommand(exportCmd)
}
