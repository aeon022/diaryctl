package cmd

import (
	"fmt"

	"github.com/aeon022/diaryctl/internal/mcpserver"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start MCP server (stdio)",
	Long:  `Start the diaryctl MCP server for use with Claude and other AI tools.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintln(cmd.ErrOrStderr(), "Starting diaryctl MCP server on stdio...")
		return mcpserver.Serve()
	},
}
