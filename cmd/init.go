package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var initName string

var initCmd = &cobra.Command{
	Use:   "init [PATH]",
	Short: "Register a git repo",
	Long:  `Register a git repository so diaryctl can track its commit history.`,
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := "."
		if len(args) > 0 {
			path = args[0]
		}

		// Resolve to absolute path.
		absPath, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("resolving path: %w", err)
		}

		// Check directory exists.
		if _, err := os.Stat(absPath); err != nil {
			return fmt.Errorf("path does not exist: %s", absPath)
		}

		// Check it's a git repo.
		if _, err := os.Stat(filepath.Join(absPath, ".git")); err != nil {
			return fmt.Errorf("not a git repository: %s", absPath)
		}

		// Default name to directory basename.
		name := initName
		if name == "" {
			name = filepath.Base(absPath)
		}

		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()

		if err := s.SaveRepo(absPath, name); err != nil {
			return fmt.Errorf("saving repo: %w", err)
		}

		fmt.Printf("✓ Registered: %s (%s)\n", name, absPath)
		return nil
	},
}

func init() {
	initCmd.Flags().StringVar(&initName, "name", "", "Display name for the repo (default: directory basename)")
}
