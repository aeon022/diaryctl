// Package actlog connects diaryctl to the suite's activity log
// (missionctl-core/activity): logging the entries the user writes, and
// putting a day's activity into that day's diary entry as a marked block.
package actlog

import (
	"sync"
	"time"

	"github.com/aeon022/diaryctl/internal/notectl"
	"github.com/aeon022/diaryctl/internal/store"
	"github.com/aeon022/missionctl-core/activity"
)

// Now is the clock; tests replace it.
var Now = time.Now

var (
	mu     sync.Mutex
	logged = map[string]bool{}
)

// LogWrote records that the user wrote the entry for date, at most once per
// date per process (the editor autosaves every 30 s and must not flood the
// log). Never call it for generated templates.
func LogWrote(date time.Time) {
	key := date.Format("2006-01-02")
	mu.Lock()
	seen := logged[key]
	logged[key] = true
	mu.Unlock()
	if !seen {
		activity.Log("diaryctl", "wrote", key)
	}
}

// Events returns the activity of date's calendar day.
func Events(date time.Time) []activity.Event {
	from, to := activity.Day(date)
	evs, _ := activity.Read(from, to)
	return evs
}

// Block is the markdown block for date's activity ("" when there is none).
func Block(date time.Time) string { return activity.MarkdownBlock(Events(date)) }

// Apply adds or refreshes the activity block in the EXISTING entry for date,
// leaving every other byte of the entry alone, and writes it back to notectl
// like every other save. It returns how many events went in; with no events
// or no entry nothing is written.
func Apply(s *store.Store, date time.Time) (int, error) {
	evs := Events(date)
	if len(evs) == 0 {
		return 0, nil
	}
	entry, err := s.GetEntry(date)
	if err != nil || entry == nil {
		return 0, err
	}
	body := activity.ReplaceBlock(entry.Body, activity.MarkdownBlock(evs))
	if body == entry.Body {
		return len(evs), nil
	}
	if err := s.SaveEntry(date, body, entry.Generated); err != nil {
		return 0, err
	}
	_ = notectl.WriteBack(date, body)
	return len(evs), nil
}
