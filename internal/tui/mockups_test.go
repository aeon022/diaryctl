package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aeon022/diaryctl/internal/store"
)

// TestRenderLandingMockups writes an ANSI screenshot of the real TUI (made-up demo data) for the landing
// page: DIARYCTL_MOCKUP_DIR=/dir go test ./internal/tui -run TestRenderLandingMockups
func TestRenderLandingMockups(t *testing.T) {
	dir := os.Getenv("DIARYCTL_MOCKUP_DIR")
	if dir == "" {
		t.Skip("set DIARYCTL_MOCKUP_DIR to render landing mockups")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DIARYCTL_DATA_DIR", "")
	s, err := store.Open(filepath.Join(t.TempDir(), "diary.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now()
	entries := []string{
		"# Landing page and theme switcher\n\n#dev #missionctl\nShipped live themes in every app, tabs on the dashboard mockup and a docs section. 14 commits across 4 repos.",
		"# Security hardening day\n\n#dev #security\nOwner-only files, shared database lock, confirm on every MCP delete.",
		"# Slow Sunday\n\n#home\nLong walk, baked bread, finished the book.",
		"# Planning Q4\n\n#work #plan\nRoadmap, priorities and a first draft of the changelog.",
		"# Mail and invoices\n\n#work\nCleared the inbox, sent two invoices, reviewed pull requests.",
		"# Terminal tools\n\n#dev\nTried a few new TUI libraries; kept the ones that feel calm.",
	}
	for i, body := range entries {
		d := time.Date(now.Year(), now.Month(), now.Day()-i, 0, 0, 0, 0, time.UTC)
		if err := s.SaveEntry(d, body, i%2 == 0); err != nil {
			t.Fatal(err)
		}
	}
	m := New(s)
	m.width, m.height = 118, 20
	run(t, m, cmdLoadEntries(s)())
	m.todayLoaded, m.todayCommits, m.todayTasks, m.todayEvents = true, 14, 6, 3 // the "Today" panel's suite numbers
	out := m.View().Content
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "diaryctl.ansi"), []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
}
