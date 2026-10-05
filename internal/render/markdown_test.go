package render

import (
	"bytes"
	"strings"
	"testing"
)

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

func TestRenderHTMLEscapesAndStructures(t *testing.T) {
	blocks := ParseMarkdown("# <script>alert(1)</script>\n\nSome **b&b** and *it*.\n\n- one\n- two\n\ntext after list\n\n---\n\n### deep")
	out := string(RenderHTML(`T"<x>`, blocks))

	for _, bad := range []string{"<script>", `<title>T"<x>`} {
		if strings.Contains(out, bad) {
			t.Errorf("unescaped %q in output:\n%s", bad, out)
		}
	}
	for _, want := range []string{
		"<title>T&#34;&lt;x&gt;</title>",
		"<h1>&lt;script&gt;alert(1)&lt;/script&gt;</h1>",
		"<b>b&amp;b</b>", "<i>it</i>",
		"<ul>\n<li>one</li>\n<li>two</li>\n</ul>\n<p>text after list</p>", // list closed before the next block
		"<hr>", "<h3>deep</h3>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
	if strings.Count(out, "<ul>") != strings.Count(out, "</ul>") {
		t.Error("unbalanced <ul>")
	}
}

func TestRenderHTMLClosesTrailingList(t *testing.T) {
	out := string(RenderHTML("t", ParseMarkdown("- a\n- b")))
	if !strings.Contains(out, "</ul>\n</body>") {
		t.Errorf("list at end of document left open:\n%s", out)
	}
}

func TestInlineSpansKeepsStrayMarkers(t *testing.T) {
	// a lone "*" or unterminated "**" must not swallow or drop text
	for _, in := range []string{"2 * 3 = 6", "**never closed", "*also open", "a ** b"} {
		if got := plainText(in); got != in {
			t.Errorf("plainText(%q) = %q, text was altered", in, got)
		}
	}
	if got := plainText("x **b** y *i* z"); got != "x b y i z" {
		t.Errorf("plainText = %q", got)
	}
}

func TestRenderPDF(t *testing.T) {
	blocks := ParseMarkdown("# Tagebuch Übersicht — Größe\n\nText mit **fett** und Emoji 🚀 sowie Umlauten äöü.\n\n- Punkt eins\n- Punkt zwei\n\n---\n\n## Ende")
	pdf, err := RenderPDF("Tagebuch — Größe", blocks)
	if err != nil {
		t.Fatalf("RenderPDF: %v", err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) || !bytes.Contains(pdf[len(pdf)-32:], []byte("%%EOF")) {
		t.Errorf("not a well-formed PDF (len %d, head %q)", len(pdf), pdf[:min(8, len(pdf))])
	}
	// empty document still yields a valid one-page PDF
	if pdf, err = RenderPDF("leer", nil); err != nil || !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Errorf("empty doc: %v", err)
	}
}
