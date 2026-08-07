package diary

import (
	"reflect"
	"testing"
)

func TestParseTitleTags_FreeformEntry(t *testing.T) {
	body := "## Migrated the Widget Service\n\n**Tags:** Day-Job, widget-service\n\nRewrote the thing.\n"
	title, tags := ParseTitleTags(body)
	if title != "Migrated the Widget Service" {
		t.Errorf("title = %q, want %q", title, "Migrated the Widget Service")
	}
	if want := []string{"Day-Job", "widget-service"}; !reflect.DeepEqual(tags, want) {
		t.Errorf("tags = %v, want %v", tags, want)
	}
}

func TestParseTitleTags_TemplateGeneratedEntry_NoUsableTitle(t *testing.T) {
	body := "# 2026-08-07\n\n## Stats\n- **Commits:** 3 across 2 repos\n\n## Commits\n- `abc123` fix bug\n"
	title, tags := ParseTitleTags(body)
	if title != "" {
		t.Errorf("title = %q, want empty — only structural headings present", title)
	}
	if tags != nil {
		t.Errorf("tags = %v, want nil", tags)
	}
}

func TestParseTitleTags_NoTagsLine(t *testing.T) {
	body := "## Untangled the deploy pipeline\n\nJust prose, no tags line.\n"
	title, tags := ParseTitleTags(body)
	if title != "Untangled the deploy pipeline" {
		t.Errorf("title = %q, want %q", title, "Untangled the deploy pipeline")
	}
	if tags != nil {
		t.Errorf("tags = %v, want nil", tags)
	}
}

func TestIsKnownCategory(t *testing.T) {
	for _, tag := range []string{"FH", "Studium", "Projekt", "Day-Job"} {
		if !IsKnownCategory(tag) {
			t.Errorf("IsKnownCategory(%q) = false, want true", tag)
		}
	}
	if IsKnownCategory("a83-next") {
		t.Errorf("IsKnownCategory(%q) = true, want false", "a83-next")
	}
}
