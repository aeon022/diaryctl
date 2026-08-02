// Package render converts the plain Markdown diaryctl already produces
// (digest.go, export.go) into HTML or PDF for sharing outside a terminal.
// It only covers the subset diaryctl itself writes — headings, paragraphs,
// "- " list items, "**bold**"/"*italic*" spans, and "---" rules — not
// full CommonMark. A dedicated Markdown library would be overkill for a
// known, self-authored input format.
package render

import "strings"

type BlockType int

const (
	Paragraph BlockType = iota
	Heading
	ListItem
	Rule
)

type Block struct {
	Type  BlockType
	Level int // heading level (1-3); unused otherwise
	Text  string
}

// ParseMarkdown splits body into blocks: consecutive non-blank, non-special
// lines join into one paragraph; blank lines separate blocks.
func ParseMarkdown(body string) []Block {
	var blocks []Block
	var para []string
	flush := func() {
		if len(para) > 0 {
			blocks = append(blocks, Block{Type: Paragraph, Text: strings.Join(para, " ")})
			para = nil
		}
	}
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			flush()
		case trimmed == "---":
			flush()
			blocks = append(blocks, Block{Type: Rule})
		case strings.HasPrefix(trimmed, "### "):
			flush()
			blocks = append(blocks, Block{Type: Heading, Level: 3, Text: trimmed[4:]})
		case strings.HasPrefix(trimmed, "## "):
			flush()
			blocks = append(blocks, Block{Type: Heading, Level: 2, Text: trimmed[3:]})
		case strings.HasPrefix(trimmed, "# "):
			flush()
			blocks = append(blocks, Block{Type: Heading, Level: 1, Text: trimmed[2:]})
		case strings.HasPrefix(trimmed, "- "), strings.HasPrefix(trimmed, "* "):
			flush()
			blocks = append(blocks, Block{Type: ListItem, Text: trimmed[2:]})
		default:
			para = append(para, trimmed)
		}
	}
	flush()
	return blocks
}

// inlineSpans splits text on "**bold**" and "*italic*" markers, in source
// order, for renderers that need per-span styling (PDF). bold==italic==false
// means plain text.
type span struct {
	text         string
	bold, italic bool
}

func inlineSpans(text string) []span {
	var out []span
	for len(text) > 0 {
		bi := strings.Index(text, "**")
		ii := strings.Index(text, "*")
		switch {
		case bi == 0:
			end := strings.Index(text[2:], "**")
			if end < 0 {
				out = append(out, span{text: text})
				return out
			}
			out = append(out, span{text: text[2 : 2+end], bold: true})
			text = text[2+end+2:]
		case ii == 0:
			end := strings.Index(text[1:], "*")
			if end < 0 {
				out = append(out, span{text: text})
				return out
			}
			out = append(out, span{text: text[1 : 1+end], italic: true})
			text = text[1+end+1:]
		default:
			next := len(text)
			if ii >= 0 && ii < next {
				next = ii
			}
			out = append(out, span{text: text[:next]})
			text = text[next:]
		}
	}
	return out
}
