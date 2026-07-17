package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// dw is the display width of a plain (unstyled) string, wide-rune aware.
func dw(s string) int { return runewidth.StringWidth(s) }

// truncTail clips s to w cells with a trailing ellipsis. Used for grid cells.
func truncTail(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return runewidth.Truncate(s, w, "…")
}

// truncMiddle clips s to w cells keeping head and tail halves around a
// single ellipsis. Used for names and paths.
func truncMiddle(s string, w int) string {
	if dw(s) <= w {
		return s
	}
	if w <= 1 {
		return truncTail(s, w)
	}
	head := (w - 1 + 1) / 2 // ceil
	tail := w - 1 - head
	left := runewidth.Truncate(s, head, "")
	right := ""
	if tail > 0 {
		runes := []rune(s)
		i := len(runes)
		width := 0
		for i > 0 && width+runewidth.RuneWidth(runes[i-1]) <= tail {
			i--
			width += runewidth.RuneWidth(runes[i])
		}
		right = string(runes[i:])
	}
	return left + "…" + right
}

// padRight pads plain string s with spaces to exactly w cells (clipping if
// longer).
func padRight(s string, w int) string {
	d := dw(s)
	if d > w {
		s = truncTail(s, w)
		d = dw(s)
	}
	return s + strings.Repeat(" ", w-d)
}

// padANSI pads a styled string to exactly w display cells, measuring with
// lipgloss (ANSI-aware), clipping crudely at the end if too long.
func padANSI(s string, w int) string {
	d := lipgloss.Width(s)
	if d > w {
		// Styled overflow should not happen; callers pre-truncate plain
		// text. Degrade by cutting visible cells if it does.
		s = truncTail(stripToWidth(s), w)
		d = lipgloss.Width(s)
	}
	return s + strings.Repeat(" ", w-d)
}

// stripToWidth is a last-resort fallback for padANSI overflow; it drops
// styling entirely.
func stripToWidth(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case inEsc:
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
		case r == '\x1b':
			inEsc = true
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// fuzzyMatch reports whether every rune of pattern appears in s in order
// (case-insensitive subsequence match).
func fuzzyMatch(pattern, s string) bool {
	p := []rune(strings.ToLower(pattern))
	if len(p) == 0 {
		return true
	}
	i := 0
	for _, r := range strings.ToLower(s) {
		if i < len(p) && r == p[i] {
			i++
		}
	}
	return i == len(p)
}
