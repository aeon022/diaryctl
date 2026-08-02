package render

import (
	"html"
	"strings"
)

// RenderHTML wraps blocks in a minimal, self-contained (no external
// stylesheet/font) HTML page suitable for sharing or printing to PDF from a
// browser.
func RenderHTML(title string, blocks []Block) []byte {
	var b strings.Builder
	b.WriteString("<!doctype html>\n<html><head><meta charset=\"utf-8\"><title>")
	b.WriteString(html.EscapeString(title))
	b.WriteString(`</title><style>
body{font-family:-apple-system,Helvetica,Arial,sans-serif;max-width:720px;margin:2rem auto;padding:0 1.5rem;line-height:1.5;color:#222}
h1{font-size:1.6rem} h2{font-size:1.3rem} h3{font-size:1.1rem}
hr{border:none;border-top:1px solid #ddd;margin:1.5rem 0}
li{margin:.25rem 0}
</style></head><body>
`)
	inList := false
	closeList := func() {
		if inList {
			b.WriteString("</ul>\n")
			inList = false
		}
	}
	for _, blk := range blocks {
		if blk.Type != ListItem {
			closeList()
		}
		switch blk.Type {
		case Heading:
			tag := "h3"
			if blk.Level == 1 {
				tag = "h1"
			} else if blk.Level == 2 {
				tag = "h2"
			}
			b.WriteString("<" + tag + ">" + inlineHTML(blk.Text) + "</" + tag + ">\n")
		case Rule:
			b.WriteString("<hr>\n")
		case ListItem:
			if !inList {
				b.WriteString("<ul>\n")
				inList = true
			}
			b.WriteString("<li>" + inlineHTML(blk.Text) + "</li>\n")
		default:
			b.WriteString("<p>" + inlineHTML(blk.Text) + "</p>\n")
		}
	}
	closeList()
	b.WriteString("</body></html>\n")
	return []byte(b.String())
}

func inlineHTML(text string) string {
	var b strings.Builder
	for _, sp := range inlineSpans(text) {
		esc := html.EscapeString(sp.text)
		switch {
		case sp.bold:
			b.WriteString("<b>" + esc + "</b>")
		case sp.italic:
			b.WriteString("<i>" + esc + "</i>")
		default:
			b.WriteString(esc)
		}
	}
	return b.String()
}
