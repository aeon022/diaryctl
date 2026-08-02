package render

import "testing"

func TestParseMarkdown(t *testing.T) {
	body := "# Title\n\nSome **bold** and *italic* text.\n\n- item one\n- item two\n\n---\n\n## Sub"
	blocks := ParseMarkdown(body)

	want := []Block{
		{Type: Heading, Level: 1, Text: "Title"},
		{Type: Paragraph, Text: "Some **bold** and *italic* text."},
		{Type: ListItem, Text: "item one"},
		{Type: ListItem, Text: "item two"},
		{Type: Rule},
		{Type: Heading, Level: 2, Text: "Sub"},
	}
	if len(blocks) != len(want) {
		t.Fatalf("got %d blocks, want %d: %+v", len(blocks), len(want), blocks)
	}
	for i, b := range blocks {
		if b != want[i] {
			t.Errorf("block %d = %+v, want %+v", i, b, want[i])
		}
	}
}

func TestInlineSpans(t *testing.T) {
	spans := inlineSpans("plain **bold** and *italic* end")
	if len(spans) != 5 {
		t.Fatalf("got %d spans, want 5: %+v", len(spans), spans)
	}
	if spans[1].text != "bold" || !spans[1].bold {
		t.Errorf("span 1 = %+v, want bold %q", spans[1], "bold")
	}
	if spans[3].text != "italic" || !spans[3].italic {
		t.Errorf("span 3 = %+v, want italic %q", spans[3], "italic")
	}
}
