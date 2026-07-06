package cmd

import (
	"fmt"
	"os"

	"github.com/aeon022/diaryctl/internal/store"
	"github.com/aeon022/diaryctl/internal/tui"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "diaryctl",
	Short: "Developer diary powered by git history",
	Long: `diaryctl is a developer diary app that reads your git history
and lets AI generate narrative diary entries.

Run without arguments to open the TUI.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()
		return tui.Run(s)
	},
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(reposCmd)
	rootCmd.AddCommand(todayCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(showCmd)
	rootCmd.AddCommand(statsCmd)
	rootCmd.AddCommand(mcpCmd)
	rootCmd.AddCommand(daemonCmd)
}

// openStore opens the default SQLite store.
func openStore() (*store.Store, error) {
	path, err := store.DefaultDBPath()
	if err != nil {
		return nil, fmt.Errorf("finding data path: %w", err)
	}
	s, err := store.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening store: %w", err)
	}
	return s, nil
}
