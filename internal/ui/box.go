package ui

import "strings"

// renderBox draws a bordered box of exactly w x h cells. The title is
// embedded in the top border (`┌─ title ────┐`), dim normally, accent when
// the box has focus. body lines are styled strings; each is padded to the
// inner width (w-4) and framed as `│ ` + line + ` │`. Extra body lines are
// clipped, missing ones padded blank.
func renderBox(title string, focused bool, w, h int, body []string) []string {
	if w < 6 {
		w = 6
	}
	if h < 2 {
		h = 2
	}
	innerW := w - 4

	titleStyle := S.Dim
	if focused {
		titleStyle = S.Accent
	}
	t := truncTail(title, w-6)
	dashes := w - 2 - dw(t) - 3 // "─ " before, " " after
	if dashes < 0 {
		dashes = 0
	}
	top := S.Dim.Render("┌─ ") + titleStyle.Render(t) + S.Dim.Render(" "+strings.Repeat("─", dashes)+"┐")

	lines := make([]string, 0, h)
	lines = append(lines, top)
	side := S.Dim.Render("│")
	for i := 0; i < h-2; i++ {
		content := ""
		if i < len(body) {
			content = body[i]
		}
		lines = append(lines, side+" "+padANSI(content, innerW)+" "+side)
	}
	lines = append(lines, S.Dim.Render("└"+strings.Repeat("─", w-2)+"┘"))
	return lines
}

// hjoin joins several equally tall blocks of lines side by side with a
// single space gap.
func hjoin(blocks ...[]string) []string {
	height := 0
	for _, b := range blocks {
		if len(b) > height {
			height = len(b)
		}
	}
	out := make([]string, height)
	for i := 0; i < height; i++ {
		parts := make([]string, 0, len(blocks))
		for _, b := range blocks {
			if i < len(b) {
				parts = append(parts, b[i])
			}
		}
		out[i] = strings.Join(parts, " ")
	}
	return out
}
