package cmd

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/aeon022/diaryctl/internal/store"
	"github.com/spf13/cobra"
)

var (
	exportDate   string
	exportFormat string
	exportYear   int
	exportOutput string
)

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export a diary entry, or a whole year, to stdout or a file",
	Long: `Export a single diary entry (default: today) to stdout, or with
--year, export every entry in that year as CSV or JSON — same
--format/--output pattern budgetctl's own "export" uses.

Examples:
  diaryctl export
  diaryctl export --date 2026-07-10
  diaryctl export --date 2026-07-10 --format post | postctl import -
  diaryctl export --year 2026 --format csv -o diary-2026.csv
  diaryctl export --year 2026 --format json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()

		if exportYear != 0 {
			return exportYearRange(s, exportYear)
		}

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

// exportYearRange writes every entry in year as CSV (default) or JSON to
// stdout or --output.
func exportYearRange(s *store.Store, year int) error {
	entries, err := s.ListEntries(100000)
	if err != nil {
		return fmt.Errorf("listing entries: %w", err)
	}

	type row struct {
		Date      string `json:"date"`
		Generated bool   `json:"generated"`
		Body      string `json:"body"`
	}
	var rows []row
	for _, e := range entries {
		if e.Date.Year() == year {
			rows = append(rows, row{Date: e.Date.Format("2006-01-02"), Generated: e.Generated, Body: e.Body})
		}
	}

	w := os.Stdout
	if exportOutput != "" {
		f, err := os.Create(exportOutput)
		if err != nil {
			return fmt.Errorf("create output file: %w", err)
		}
		defer f.Close()
		w = f
	}

	switch exportFormat {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	default: // csv (also the fallback if --format was left at "raw"/"post", which don't apply in bulk mode)
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"date", "generated", "body"})
		for _, r := range rows {
			_ = cw.Write([]string{r.Date, fmt.Sprintf("%t", r.Generated), r.Body})
		}
		cw.Flush()
		if exportOutput != "" {
			fmt.Fprintf(os.Stderr, "Exported %d entries → %s\n", len(rows), exportOutput)
		}
		return cw.Error()
	}
}

func init() {
	exportCmd.Flags().StringVar(&exportDate, "date", "", "Date to export (YYYY-MM-DD, default: today)")
	exportCmd.Flags().StringVar(&exportFormat, "format", "raw", "Output format: raw|post (single entry) or csv|json (with --year)")
	exportCmd.Flags().IntVar(&exportYear, "year", 0, "Export every entry in this year instead of a single date")
	exportCmd.Flags().StringVarP(&exportOutput, "output", "o", "", "Output file for --year export (default: stdout)")
	rootCmd.AddCommand(exportCmd)
}
