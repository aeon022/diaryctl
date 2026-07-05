package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var reposCmd = &cobra.Command{
	Use:   "repos",
	Short: "List registered git repos",
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
			fmt.Println("No repos registered.")
			fmt.Println("Run `diaryctl init [path]` to register a git repo.")
			return nil
		}

		fmt.Printf("%-4s  %-20s  %s\n", "ID", "NAME", "PATH")
		fmt.Printf("%-4s  %-20s  %s\n", "----", "--------------------", "----")
		for _, r := range repos {
			fmt.Printf("%-4d  %-20s  %s\n", r.ID, r.Name, r.Path)
		}
		return nil
	},
}
