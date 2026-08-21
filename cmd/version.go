package cmd

// Version is set at build time via -ldflags "-X github.com/aeon022/diaryctl/cmd.Version=v1.2.3".
var Version = "dev"

func init() {
	rootCmd.Version = Version
}
