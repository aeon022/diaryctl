package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/aeon022/diaryctl/internal/actlog"
	"github.com/aeon022/missionctl-core/activity"
)

// askModel returns a model on a store whose newest entry is today's, with
// the activity log in a temp dir, the clock at hour:00 today and the diary
// mode set — everything the day-end prompt needs, then reloaded so the
// prompt logic runs.
func askModel(t *testing.T, hour int, mode activity.DiaryMode, events int, bodies ...string) *Model {
	t.Helper()
	m, s, _ := newState(t, bodies...) // sets an isolated HOME
	t.Setenv("MISSIONCTL_DATA_DIR", t.TempDir())
	t.Setenv("MISSIONCTL_ACTIVITY", "")
	if err := activity.SetDiaryMode(mode); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < events; i++ {
		activity.Log("taskctl", "completed", "Aufgabe")
	}
	now := time.Now()
	orig := actlog.Now
	actlog.Now = func() time.Time { return time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, now.Location()) }
	t.Cleanup(func() { actlog.Now = orig })
	run(t, m, cmdLoadEntries(s)())
	return m
}

func TestAskPromptAppearsWhenEverythingLinesUp(t *testing.T) {
	m := askModel(t, 19, activity.DiaryAsk, 3, "heute")
	if !m.actAsk || m.actAskCount != 3 {
		t.Fatalf("prompt: ask=%v count=%d", m.actAsk, m.actAskCount)
	}
	out := m.viewContent()
	if !strings.Contains(out, "Add today's activity (3 events)") || !strings.Contains(out, "y add now") {
		t.Errorf("popup not rendered:\n%s", out)
	}
}

func TestAskPromptStaysAwayUnlessAllConditionsHold(t *testing.T) {
	cases := []struct {
		name   string
		hour   int
		mode   activity.DiaryMode
		events int
		bodies []string
	}{
		{"before 18:00", 17, activity.DiaryAsk, 2, []string{"heute"}},
		{"auto mode", 19, activity.DiaryAuto, 2, []string{"heute"}},
		{"off mode", 19, activity.DiaryOff, 2, []string{"heute"}},
		{"no activity", 19, activity.DiaryAsk, 0, []string{"heute"}},
		{"block already there", 19, activity.DiaryAsk, 2, []string{"heute\n\n<!-- activity:start -->\nx\n<!-- activity:end -->"}},
		{"no entry for today (newest is yesterday)", 19, activity.DiaryAsk, 2, []string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := askModel(t, c.hour, c.mode, c.events, c.bodies...)
			if m.actAsk {
				t.Error("prompt must not appear")
			}
		})
	}
}

func TestAskPromptNotWhileEditingSearchingOrDeleting(t *testing.T) {
	for name, setup := range map[string]func(m *Model){
		"editor":         func(m *Model) { m.view = editorView },
		"search":         func(m *Model) { m.searching = true },
		"palette":        func(m *Model) { m.inPalette = true },
		"delete confirm": func(m *Model) { m.confirmDelete = true },
	} {
		t.Run(name, func(t *testing.T) {
			m := askModel(t, 8, activity.DiaryAsk, 2, "heute") // 8:00 → no prompt yet
			setup(m)
			actlog.Now = func() time.Time { return time.Now().Truncate(24 * time.Hour).Add(19 * time.Hour) }
			m.maybeAskActivity()
			if m.actAsk {
				t.Errorf("prompt opened while %s", name)
			}
		})
	}
}

func entryBody(t *testing.T, m *Model) string {
	t.Helper()
	e, err := m.store.GetEntry(time.Now())
	if err != nil || e == nil {
		t.Fatalf("entry: %v %v", e, err)
	}
	return e.Body
}

func TestAskPromptKeys(t *testing.T) {
	t.Run("y adds the block and keeps the mode", func(t *testing.T) {
		m := askModel(t, 19, activity.DiaryAsk, 2, "mein Text")
		keys(t, m, "y")
		body := entryBody(t, m)
		if m.actAsk || !activity.HasBlock(body) || !strings.HasPrefix(body, "mein Text") {
			t.Errorf("ask=%v body=%q", m.actAsk, body)
		}
		if activity.Load().Diary != activity.DiaryAsk {
			t.Error("y must not change the mode")
		}
	})
	for _, k := range []string{"n", "esc"} {
		t.Run(k+" is 'not now'", func(t *testing.T) {
			m := askModel(t, 19, activity.DiaryAsk, 2, "mein Text")
			keys(t, m, k)
			if m.actAsk || activity.HasBlock(entryBody(t, m)) || activity.Load().Diary != activity.DiaryAsk {
				t.Errorf("%s: nothing may change (ask=%v)", k, m.actAsk)
			}
		})
	}
	t.Run("a = always: auto mode + add now", func(t *testing.T) {
		m := askModel(t, 19, activity.DiaryAsk, 2, "mein Text")
		keys(t, m, "a")
		if activity.Load().Diary != activity.DiaryAuto || !activity.HasBlock(entryBody(t, m)) {
			t.Errorf("mode=%s block=%v", activity.Load().Diary, activity.HasBlock(entryBody(t, m)))
		}
	})
	t.Run("x = never: off, nothing added", func(t *testing.T) {
		m := askModel(t, 19, activity.DiaryAsk, 2, "mein Text")
		keys(t, m, "x")
		if activity.Load().Diary != activity.DiaryOff || activity.HasBlock(entryBody(t, m)) || m.actAsk {
			t.Errorf("mode=%s", activity.Load().Diary)
		}
	})
	t.Run("other keys are swallowed while the popup is open", func(t *testing.T) {
		m := askModel(t, 19, activity.DiaryAsk, 2, "heute", "gestern")
		keys(t, m, "j")
		if !m.actAsk || m.cursor != 0 {
			t.Errorf("popup=%v cursor=%d: j must not reach the list", m.actAsk, m.cursor)
		}
	})
}

func TestAskPromptOnlyOncePerSession(t *testing.T) {
	m := askModel(t, 19, activity.DiaryAsk, 2, "heute")
	keys(t, m, "n")
	run(t, m, cmdLoadEntries(m.store)()) // e.g. a focus reload
	if m.actAsk {
		t.Error("asked a second time in the same session")
	}
}
