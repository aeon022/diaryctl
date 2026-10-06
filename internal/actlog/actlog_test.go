package actlog

import (
	"strings"
	"testing"
	"time"

	"github.com/aeon022/diaryctl/internal/store"
	"github.com/aeon022/missionctl-core/activity"
)

func sandbox(t *testing.T) *store.Store {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MISSIONCTL_DATA_DIR", t.TempDir())
	t.Setenv("MISSIONCTL_ACTIVITY", "")
	s, err := store.Open(t.TempDir()+"/diary.db", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestLogWroteOncePerDatePerProcess(t *testing.T) {
	sandbox(t)
	logged = map[string]bool{}
	d := time.Now()
	LogWrote(d)
	LogWrote(d)
	LogWrote(d.AddDate(0, 0, -1))
	evs, _ := activity.Read(time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	if len(evs) != 2 || evs[0].Tool != "diaryctl" || evs[0].Action != "wrote" {
		t.Errorf("want one event per date (2), got %+v", evs)
	}
}

func TestApplyAddsRefreshesAndKeepsUserText(t *testing.T) {
	s := sandbox(t)
	day := time.Now()
	if err := s.SaveEntry(day, "# Mein Tag\n\nAlles von mir.", false); err != nil {
		t.Fatal(err)
	}
	if n, err := Apply(s, day); n != 0 || err != nil {
		t.Fatalf("no events → nothing written: %d %v", n, err)
	}
	activity.Log("taskctl", "completed", "Steuer")
	if n, err := Apply(s, day); n != 1 || err != nil {
		t.Fatalf("Apply = %d, %v", n, err)
	}
	activity.Log("habctl", "checked", "Sport")
	if n, _ := Apply(s, day); n != 2 {
		t.Fatalf("refresh should see both events, got %d", n)
	}
	e, _ := s.GetEntry(day)
	if strings.Count(e.Body, "<!-- activity:start -->") != 1 || !strings.HasPrefix(e.Body, "# Mein Tag\n\nAlles von mir.") ||
		!strings.Contains(e.Body, "Steuer") || !strings.Contains(e.Body, "Sport") {
		t.Errorf("body after refresh:\n%s", e.Body)
	}
	if n, err := Apply(s, day.AddDate(0, 0, -3)); n != 0 || err != nil {
		t.Errorf("a day without events/entry must be a no-op, got %d %v", n, err)
	}
}
