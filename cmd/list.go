package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var (
	listLimit int
	listJSON  bool
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List past diary entries",
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()

		entries, err := s.ListEntries(listLimit)
		if err != nil {
			return fmt.Errorf("listing entries: %w", err)
		}

		if listJSON {
			type entryOut struct {
				Date       string `json:"date"`
				FirstLine  string `json:"first_line"`
				HasContent bool   `json:"has_content"`
				Generated  bool   `json:"generated"`
			}
			var out []entryOut
			for _, e := range entries {
				out = append(out, entryOut{
					Date:       e.Date.Format("2006-01-02"),
					FirstLine:  firstNonEmptyLine(e.Body),
					HasContent: len(strings.TrimSpace(e.Body)) > 0,
					Generated:  e.Generated,
				})
			}
			b, _ := json.MarshalIndent(out, "", "  ")
			fmt.Println(string(b))
			return nil
		}

		if len(entries) == 0 {
			fmt.Println("No entries yet. Run `diaryctl today` to generate today's entry.")
			return nil
		}

		fmt.Printf("%-12s  %-40s  %-9s  %s\n", "DATE", "PREVIEW", "HAS BODY", "SOURCE")
		fmt.Printf("%-12s  %-40s  %-9s  %s\n", "------------", "----------------------------------------", "---------", "------")
		for _, e := range entries {
			preview := firstNonEmptyLine(e.Body)
			if len(preview) > 40 {
				preview = preview[:37] + "..."
			}
			hasBody := "no"
			if len(strings.TrimSpace(e.Body)) > 0 {
				hasBody = "yes"
			}
			source := "manual"
			if e.Generated {
				source = "AI"
			}
			fmt.Printf("%-12s  %-40s  %-9s  %s\n",
				e.Date.Format("2006-01-02"), preview, hasBody, source)
		}
		return nil
	},
}

func init() {
	listCmd.Flags().IntVar(&listLimit, "limit", 30, "Maximum number of entries to show")
	listCmd.Flags().BoolVar(&listJSON, "json", false, "Output as JSON")
}

func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return "(empty)"
}
