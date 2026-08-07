package diary

import (
	"regexp"
	"strings"
)

// templateSectionNames are BuildEntryBody's own section headings — never
// usable as a title, since they're structural, not descriptive.
var templateSectionNames = map[string]bool{
	"Stats": true, "Calendar": true, "Completed Tasks": true,
	"Time Log": true, "Habits": true, "Commits": true,
	"Context": true, "Reflection": true, "Tomorrow": true,
}

var (
	headingRe = regexp.MustCompile(`^#{1,3}\s*(.+?)\s*$`)
	dateRe    = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	tagsRe    = regexp.MustCompile(`(?m)^\*\*Tags:\*\*\s*(.+)$`)
)

// ParseTitleTags extracts an entry's title and tags directly from its
// markdown body, rather than a separate stored field — the two real
// entries this was designed against already write "## <descriptive
// title>" as their opening line and a "**Tags:** a, b" line further down
// (a convention that grew organically before this feature existed), so
// treating the body as the single source of truth needed no migration and
// stays in sync automatically if the heading is ever edited by hand.
//
// title is "" when the body's only heading is the plain "# 2026-08-07"
// date stamp or one of BuildEntryBody's own section names (Stats,
// Commits, ...) — i.e. a template-generated entry nobody has titled yet.
// Callers should fall back to their own preview (e.g. firstLine) in that
// case.
func ParseTitleTags(body string) (title string, tags []string) {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "#") {
			continue
		}
		m := headingRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		text := m[1]
		if dateRe.MatchString(text) || templateSectionNames[text] {
			continue
		}
		title = text
		break
	}

	if m := tagsRe.FindStringSubmatch(body); m != nil {
		for _, t := range strings.Split(m[1], ",") {
			if t = strings.TrimSpace(t); t != "" {
				tags = append(tags, t)
			}
		}
	}
	return title, tags
}

// knownCategories are the example life-categories the user tags entries
// with (FH, Studium, Projekt, Day-Job, ...) — recognized ones get a
// distinct accent color in the UI so they read as "which part of life"
// at a glance; anything else (a specific project name, say) still shows,
// just in the plain muted tag style.
var knownCategories = map[string]bool{
	"FH": true, "Studium": true, "Projekt": true, "Day-Job": true,
}

// IsKnownCategory reports whether tag is one of the recognized
// life-category tags (case-sensitive, matching how they're written).
func IsKnownCategory(tag string) bool {
	return knownCategories[tag]
}
