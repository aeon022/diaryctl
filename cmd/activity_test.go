package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/aeon022/missionctl-core/activity"
)

// activitySandbox isolates HOME, the diary DB and the activity log, and clears
// every AI key so `daemon generate` never calls out.
func activitySandbox(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DIARYCTL_DATA_DIR", t.TempDir())
	t.Setenv("MISSIONCTL_DATA_DIR", t.TempDir())
	t.Setenv("MISSIONCTL_ACTIVITY", "")
	for _, k := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY", "DIARYCTL_PROVIDER"} {
		t.Setenv(k, "")
	}
}

func todayBody(t *testing.T) string {
	t.Helper()
	s, err := openStore()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e, _ := s.GetEntry(time.Now())
	if e == nil {
		return ""
	}
	return e.Body
}

func generate(t *testing.T) {
	t.Helper()
	if err := daemonGenerateCmd.RunE(daemonGenerateCmd, nil); err != nil {
		t.Fatalf("generate: %v", err)
	}
}

func TestDaemonGenerateAutoAddsBlockToNewEntry(t *testing.T) {
	activitySandbox(t)
	_ = activity.SetDiaryMode(activity.DiaryAuto)
	activity.Log("taskctl", "completed", "Steuer")
	generate(t)
	if body := todayBody(t); !activity.HasBlock(body) || !strings.Contains(body, "Steuer") {
		t.Errorf("new entry should carry the activity block:\n%s", body)
	}
}

func TestDaemonGenerateAutoRefreshesOnlyTheBlockOfAnExistingEntry(t *testing.T) {
	activitySandbox(t)
	s, _ := openStore()
	_ = s.SaveEntry(time.Now(), "# Mein Eintrag\n\nHandgeschrieben.", false)
	s.Close()
	_ = activity.SetDiaryMode(activity.DiaryAuto)

	activity.Log("taskctl", "completed", "Erstes")
	generate(t)
	activity.Log("habctl", "checked", "Zweites")
	generate(t) // refresh, not append

	body := todayBody(t)
	if !strings.HasPrefix(body, "# Mein Eintrag\n\nHandgeschrieben.") {
		t.Errorf("user text must be untouched:\n%s", body)
	}
	if strings.Count(body, "<!-- activity:start -->") != 1 || !strings.Contains(body, "Erstes") || !strings.Contains(body, "Zweites") {
		t.Errorf("block must be refreshed in place with both events:\n%s", body)
	}
}

func TestDaemonGenerateAskAndOffLeaveExistingEntryAlone(t *testing.T) {
	for _, mode := range []activity.DiaryMode{activity.DiaryAsk, activity.DiaryOff} {
		activitySandbox(t)
		s, _ := openStore()
		_ = s.SaveEntry(time.Now(), "Nur mein Text", false)
		s.Close()
		_ = activity.SetDiaryMode(mode)
		activity.Log("taskctl", "completed", "x")
		generate(t)
		if body := todayBody(t); body != "Nur mein Text" {
			t.Errorf("mode %s changed the entry: %q", mode, body)
		}
	}
}

func TestActivityCommandSetsModeAndAddsBlock(t *testing.T) {
	activitySandbox(t)
	reset := func() { activityMode, activityDate = "", ""; activityCmd.Flags().Lookup("mode").Changed = false }
	t.Cleanup(reset)

	// mode switching
	for _, m := range []string{"auto", "off", "ask"} {
		reset()
		_ = activityCmd.Flags().Set("mode", m)
		if err := activityCmd.RunE(activityCmd, nil); err != nil || string(activity.Load().Diary) != m {
			t.Fatalf("--mode %s: err=%v mode=%s", m, err, activity.Load().Diary)
		}
	}
	reset()
	_ = activityCmd.Flags().Set("mode", "sometimes")
	if err := activityCmd.RunE(activityCmd, nil); err == nil {
		t.Error("an unknown mode must be rejected")
	}

	// no entry yet → clear error; with entry → block added in any mode
	reset()
	activity.Log("notectl", "wrote", "Notiz")
	if err := activityCmd.RunE(activityCmd, nil); err == nil || !strings.Contains(err.Error(), "no diary entry") {
		t.Errorf("without an entry: %v", err)
	}
	s, _ := openStore()
	_ = s.SaveEntry(time.Now(), "Text", false)
	s.Close()
	if err := activityCmd.RunE(activityCmd, nil); err != nil {
		t.Fatal(err)
	}
	if body := todayBody(t); !activity.HasBlock(body) || !strings.Contains(body, "Notiz") {
		t.Errorf("block missing:\n%s", body)
	}
	_ = activityCmd.Flags().Set("date", "kaputt")
	if err := activityCmd.RunE(activityCmd, nil); err == nil {
		t.Error("bad --date must be rejected")
	}
}
