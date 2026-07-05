package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var showCmd = &cobra.Command{
	Use:   "show [DATE]",
	Short: "Show a specific diary entry",
	Long:  `Show a diary entry. DATE should be in YYYY-MM-DD format. Defaults to today.`,
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()

		date := time.Now()
		if len(args) > 0 {
			date, err = time.Parse("2006-01-02", args[0])
			if err != nil {
				return fmt.Errorf("invalid date format, use YYYY-MM-DD: %w", err)
			}
		}

		entry, err := s.GetEntry(date)
		if err != nil {
			return fmt.Errorf("reading entry: %w", err)
		}

		if entry == nil || entry.Body == "" {
			fmt.Printf("No entry for %s.\n", date.Format("2006-01-02"))
			fmt.Println("Run `diaryctl today` to generate today's entry.")
			return nil
		}

		fmt.Println(entry.Body)
		return nil
	},
}
