package cmd

import (
	"fmt"
	"time"

	"github.com/aeon022/diaryctl/internal/actlog"
	"github.com/aeon022/missionctl-core/activity"
	"github.com/spf13/cobra"
)

var (
	activityDate string
	activityMode string
)

var activityCmd = &cobra.Command{
	Use:   "activity",
	Short: "Add a day's suite activity to its diary entry, or set how that happens",
	Long: `Adds (or refreshes) the day's activity from the suite's activity log
(tasks done, habits checked, notes written, ...) as a marked block at the end of
that day's diary entry. Only the block between <!-- activity:start --> and
<!-- activity:end --> is ever replaced; the rest of the entry is never touched.

--mode controls the automatic behavior:
  ask   offer to add it in the TUI in the evening (default)
  auto  add it when the daily entry is generated (daemon)
  off   never`,
	Example: `  diaryctl activity                 # add today's activity now
  diaryctl activity --date 2026-10-05
  diaryctl activity --mode auto     # let the daemon do it at day end`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("mode") {
			switch m := activity.DiaryMode(activityMode); m {
			case activity.DiaryAsk, activity.DiaryAuto, activity.DiaryOff:
				if err := activity.SetDiaryMode(m); err != nil {
					return err
				}
				fmt.Printf("✓ Activity in the diary: %s\n", m)
				return nil
			default:
				return fmt.Errorf("--mode must be ask, auto or off")
			}
		}

		date := actlog.Now()
		if activityDate != "" {
			d, err := time.ParseInLocation("2006-01-02", activityDate, time.Local)
			if err != nil {
				return fmt.Errorf("--date: use YYYY-MM-DD")
			}
			date = d
		}
		st := activity.Load()
		if !st.Enabled {
			fmt.Println("Activity logging is off (MISSIONCTL_ACTIVITY or activity.yaml) — nothing to add.")
			return nil
		}
		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()
		if e, _ := s.GetEntry(date); e == nil {
			return fmt.Errorf("no diary entry for %s yet — create it first (diaryctl today)", date.Format("2006-01-02"))
		}
		n, err := actlog.Apply(s, date)
		if err != nil {
			return err
		}
		if n == 0 {
			fmt.Printf("No activity logged for %s (mode: %s).\n", date.Format("2006-01-02"), st.Diary)
			return nil
		}
		fmt.Printf("✓ %d events added to the %s entry (mode: %s)\n", n, date.Format("2006-01-02"), st.Diary)
		return nil
	},
}

func init() {
	activityCmd.Flags().StringVar(&activityDate, "date", "", "Day to add (YYYY-MM-DD, default today)")
	activityCmd.Flags().StringVar(&activityMode, "mode", "", "Set the automatic mode: ask | auto | off")
	rootCmd.AddCommand(activityCmd)
}
