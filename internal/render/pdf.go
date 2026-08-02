package render

import (
	"bytes"

	"github.com/go-pdf/fpdf"
)

// RenderPDF lays blocks out directly as PDF text — simpler and more
// portable than going through HTML, since macOS has no reliable built-in
// HTML→PDF conversion (cupsfilter's html filter isn't wired up by default,
// textutil doesn't support pdf as an output format, and wkhtmltopdf isn't
// installed — confirmed by testing, not assumed).
func RenderPDF(title string, blocks []Block) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetTitle(title, true)
	pdf.SetMargins(20, 20, 20)
	pdf.AddPage()
	const lineW = 170 // usable width in mm at these margins on A4

	// The core Helvetica font expects cp1252 bytes, not UTF-8 — without this
	// translation, umlauts/em-dashes/bullets render as mojibake (confirmed
	// by testing). cp1252 covers German diary text; anything outside it
	// (emoji, etc.) silently drops rather than corrupting the page.
	tr := pdf.UnicodeTranslatorFromDescriptor("")

	pdf.SetFont("Helvetica", "B", 18)
	pdf.MultiCell(lineW, 8, tr(title), "", "L", false)
	pdf.Ln(4)

	for _, blk := range blocks {
		switch blk.Type {
		case Heading:
			size := 14.0
			if blk.Level == 2 {
				size = 12.5
			} else if blk.Level >= 3 {
				size = 11.5
			}
			pdf.Ln(2)
			pdf.SetFont("Helvetica", "B", size)
			pdf.MultiCell(lineW, 6, tr(blk.Text), "", "L", false)
		case Rule:
			y := pdf.GetY() + 2
			pdf.Line(20, y, 20+lineW, y)
			pdf.Ln(6)
		case ListItem:
			pdf.SetFont("Helvetica", "", 11)
			pdf.SetX(24)
			pdf.MultiCell(lineW-4, 5.5, "-  "+tr(plainText(blk.Text)), "", "L", false)
		default:
			pdf.SetFont("Helvetica", "", 11)
			pdf.MultiCell(lineW, 5.5, tr(plainText(blk.Text)), "", "L", false)
			pdf.Ln(2)
		}
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// plainText drops the bold/italic markers — fpdf's plain Cell/MultiCell
// calls don't do inline run styling, and a diary/digest export doesn't need
// it badly enough to justify fpdf's more involved per-run Write() API.
func plainText(text string) string {
	var out []byte
	for _, sp := range inlineSpans(text) {
		out = append(out, sp.text...)
	}
	return string(out)
}
