package cmd

import (
	"strings"
	"testing"
)

func TestBarStr(t *testing.T) {
	cases := []struct {
		count, max, width int
		filled            int
	}{
		{0, 10, 10, 0}, {5, 10, 10, 5}, {10, 10, 10, 10},
		{20, 10, 10, 10}, // over max is clamped, never wider than width
		{3, 0, 8, 0},     // max 0 (no data) must not divide by zero
	}
	for _, c := range cases {
		got := barStr(c.count, c.max, c.width)
		if n := len([]rune(got)); n != c.width {
			t.Errorf("barStr(%d,%d,%d) width = %d, want %d", c.count, c.max, c.width, n, c.width)
		}
		if f := strings.Count(got, "█"); f != c.filled {
			t.Errorf("barStr(%d,%d,%d) filled = %d, want %d", c.count, c.max, c.width, f, c.filled)
		}
	}
}

func TestFirstNonEmptyLine(t *testing.T) {
	for in, want := range map[string]string{
		"":                     "(empty)",
		"  \n\t\n":             "(empty)",
		"first\nsecond":        "first",
		"\n\n  padded  \nnext": "padded",
	} {
		if got := firstNonEmptyLine(in); got != want {
			t.Errorf("firstNonEmptyLine(%q) = %q, want %q", in, got, want)
		}
	}
}
