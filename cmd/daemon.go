package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"text/template"
	"time"

	"github.com/aeon022/diaryctl/internal/ai"
	"github.com/aeon022/diaryctl/internal/diary"
	"github.com/aeon022/diaryctl/internal/git"
	"github.com/aeon022/diaryctl/internal/notectl"
	"github.com/aeon022/diaryctl/internal/suite"
	"github.com/spf13/cobra"
)

// launchd plist template (macOS only).
const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
    "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>sh.missionctl.diaryctl</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{.Binary}}</string>
        <string>daemon</string>
        <string>generate</string>
    </array>
    <key>StartCalendarInterval</key>
    <dict>
        <key>Hour</key>
        <integer>{{.Hour}}</integer>
        <key>Minute</key>
        <integer>{{.Minute}}</integer>
    </dict>
    <key>StandardOutPath</key>
    <string>{{.LogDir}}/diaryctl.log</string>
    <key>StandardErrorPath</key>
    <string>{{.LogDir}}/diaryctl.error.log</string>
    <key>RunAtLoad</key>
    <false/>
</dict>
</plist>
`

const plistLabel = "sh.missionctl.diaryctl"

func plistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", plistLabel+".plist"), nil
}

func logDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".local", "share", "diaryctl", "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

var (
	daemonHour   int
	daemonMinute int
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the diaryctl background daemon",
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Install and start the daily diary generation daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		if runtime.GOOS != "darwin" {
			return fmt.Errorf("daemon is only supported on macOS (uses launchd)")
		}

		binary, err := os.Executable()
		if err != nil {
			return fmt.Errorf("finding binary path: %w", err)
		}
		absPath, err := filepath.Abs(binary)
		if err != nil {
			return fmt.Errorf("resolving binary path: %w", err)
		}

		logD, err := logDir()
		if err != nil {
			return err
		}

		plist, err := plistPath()
		if err != nil {
			return err
		}

		tmpl, err := template.New("plist").Parse(plistTemplate)
		if err != nil {
			return err
		}

		f, err := os.Create(plist)
		if err != nil {
			return fmt.Errorf("writing plist to %s: %w", plist, err)
		}
		defer f.Close()

		if err := tmpl.Execute(f, map[string]interface{}{
			"Binary": absPath,
			"Hour":   daemonHour,
			"Minute": daemonMinute,
			"LogDir": logD,
		}); err != nil {
			return err
		}

		// Load via launchctl.
		out, err := exec.Command("launchctl", "load", "-w", plist).CombinedOutput()
		if err != nil {
			return fmt.Errorf("launchctl load failed: %s", string(out))
		}

		fmt.Printf("✓ Daemon installed — runs daily at %02d:%02d\n", daemonHour, daemonMinute)
		fmt.Printf("  Plist: %s\n", plist)
		fmt.Printf("  Logs:  %s/diaryctl.log\n", logD)
		fmt.Println("\nTemplate is generated automatically. Open Claude and say")
		fmt.Println("\"write my diary for today\" to fill in the narrative.")
		return nil
	},
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Unload and remove the daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		if runtime.GOOS != "darwin" {
			return fmt.Errorf("daemon is only supported on macOS")
		}

		plist, err := plistPath()
		if err != nil {
			return err
		}

		out, err := exec.Command("launchctl", "unload", "-w", plist).CombinedOutput()
		if err != nil {
			// Not loaded — still remove the file.
			fmt.Printf("Note: %s\n", string(out))
		}

		if err := os.Remove(plist); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing plist: %w", err)
		}

		fmt.Println("✓ Daemon stopped and removed")
		return nil
	},
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon status",
	RunE: func(cmd *cobra.Command, args []string) error {
		if runtime.GOOS != "darwin" {
			return fmt.Errorf("daemon is only supported on macOS")
		}

		plist, err := plistPath()
		if err != nil {
			return err
		}

		if _, err := os.Stat(plist); os.IsNotExist(err) {
			fmt.Println("Daemon: not installed")
			fmt.Println("Run: diaryctl daemon start")
			return nil
		}

		out, _ := exec.Command("launchctl", "list", plistLabel).CombinedOutput()
		fmt.Printf("Plist: %s\n\n", plist)
		fmt.Println(string(out))
		return nil
	},
}

// daemonGenerateCmd is called by launchd — generates today's template silently.
var daemonGenerateCmd = &cobra.Command{
	Use:    "generate",
	Short:  "Generate today's diary template (called by daemon)",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()

		today := time.Now()

		// Skip if entry already exists and is non-empty.
		existing, _ := s.GetEntry(today)
		if existing != nil && existing.Body != "" {
			fmt.Println("Entry already exists for today — skipping")
			return nil
		}

		repos, err := s.ListRepos()
		if err != nil {
			return err
		}

		ds, err := git.DayStats(repos, today)
		if err != nil {
			return fmt.Errorf("reading git stats: %w", err)
		}
		byRepo, _ := git.CommitsByRepo(repos, today)
		ds.ByRepo = byRepo

		streak, _ := s.GetStreak()
		ds.Streak = streak

		tasks, _ := suite.TodayTasks()
		events, _ := suite.TodayEvents()
		times, _ := suite.TodayTimeEntries()

		body := diary.BuildEntryBody(ds, tasks, events, times)

		// If ANTHROPIC_API_KEY is set, let Claude write the narrative automatically.
		notif := "Your diary template is ready. Open Claude Desktop to write the narrative."
		if filled, err := ai.Fill(body); err == nil {
			body = filled
			fmt.Println("✓ Claude wrote the narrative")
			notif = "Your diary entry for today is written. Open diaryctl to review."
		} else if err != ai.ErrNoAPIKey {
			fmt.Printf("⚠ Claude error: %v — saving template only\n", err)
		}

		if err := s.SaveEntry(today, body, false); err != nil {
			return fmt.Errorf("saving entry: %w", err)
		}
		_ = notectl.WriteBack(today, body)

		fmt.Printf("✓ Diary entry saved for %s\n", today.Format("2006-01-02"))

		// macOS notification.
		if runtime.GOOS == "darwin" {
			exec.Command("osascript", "-e",
				fmt.Sprintf(`display notification %q with title "diaryctl"`, notif),
			).Run()
		}

		return nil
	},
}

func init() {
	daemonStartCmd.Flags().IntVar(&daemonHour, "hour", 17, "Hour to run (24h)")
	daemonStartCmd.Flags().IntVar(&daemonMinute, "minute", 30, "Minute to run")

	daemonCmd.AddCommand(daemonStartCmd)
	daemonCmd.AddCommand(daemonStopCmd)
	daemonCmd.AddCommand(daemonStatusCmd)
	daemonCmd.AddCommand(daemonGenerateCmd)
}
