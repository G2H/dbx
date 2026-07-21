package ui

import (
	"strings"

	"github.com/mattn/go-runewidth"

	"dbx/internal/query"
)

type cellStyle int

const (
	cellText cellStyle = iota
	cellDim
	cellNull   // dim ∅
	cellAccent // e.g. the ▌ PK marker
)

// GCell is one display cell of a grid.
type GCell struct {
	Text  string
	Style cellStyle
}

const (
	minColWidth = 3
	maxColWidth = 40
)

// Grid renders tabular data as a box whose top border carries the column
// headers. It owns cursor row, vertical offset and whole-column horizontal
// scrolling.
type Grid struct {
	Cols      []string
	Rows      [][]GCell
	Cursor    int // selected row; -1 disables the selection bar
	ColCursor int // highlighted column (details view); -1 disables

	widths []int
	rowOff int
	colOff int
}

// NewGrid builds a grid and precomputes column widths.
func NewGrid(cols []string, rows [][]GCell) *Grid {
	g := &Grid{Cols: cols, Rows: rows, Cursor: -1, ColCursor: -1}
	g.computeWidths()
	return g
}

// GridFromResult converts a scanned query result into a display grid.
func GridFromResult(res *query.Result) *Grid {
	rows := make([][]GCell, len(res.Rows))
	for i, r := range res.Rows {
		row := make([]GCell, len(r))
		for j, c := range r {
			if c.Null {
				row[j] = GCell{Text: glyphNull, Style: cellNull}
			} else {
				row[j] = GCell{Text: c.Text}
			}
		}
		rows[i] = row
	}
	return NewGrid(res.Columns, rows)
}

func (g *Grid) computeWidths() {
	g.widths = make([]int, len(g.Cols))
	for i, c := range g.Cols {
		w := dw(c)
		for _, row := range g.Rows {
			if i < len(row) && dw(row[i].Text) > w {
				w = dw(row[i].Text)
			}
		}
		if w < minColWidth {
			w = minColWidth
		}
		if w > maxColWidth {
			w = maxColWidth
		}
		g.widths[i] = w
	}
}

// Move moves the row cursor by d, clamped.
func (g *Grid) Move(d int) {
	if len(g.Rows) == 0 {
		g.Cursor = -1
		return
	}
	g.Cursor = clamp(g.Cursor+d, 0, len(g.Rows)-1)
}

// MoveCol moves the column cursor by d, clamped.
func (g *Grid) MoveCol(d int) {
	if len(g.Cols) == 0 {
		return
	}
	if g.ColCursor < 0 {
		g.ColCursor = 0
	}
	g.ColCursor = clamp(g.ColCursor+d, 0, len(g.Cols)-1)
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// visibleCols returns the column indexes that fit starting at colOff, and
// their render widths (the first column clamps to the available space).
func (g *Grid) visibleCols(avail int) ([]int, []int) {
	var idx []int
	var widths []int
	used := 0
	for i := g.colOff; i < len(g.Cols); i++ {
		w := g.widths[i]
		gap := 0
		if len(idx) > 0 {
			gap = 2
		}
		if used+gap+w > avail {
			if len(idx) == 0 {
				// A single column wider than the box clamps to it.
				idx = append(idx, i)
				widths = append(widths, avail)
			} else if left := avail - used - gap; left >= 8 {
				// The last visible column flexes into the leftover space
				// instead of disappearing entirely.
				idx = append(idx, i)
				widths = append(widths, left)
			}
			break
		}
		idx = append(idx, i)
		widths = append(widths, w)
		used += gap + w
	}
	return idx, widths
}

// Render draws the grid as a full box of w x h cells.
func (g *Grid) Render(w, h int, focused bool) []string {
	if w < 8 {
		w = 8
	}
	if h < 2 {
		h = 2
	}
	bodyH := h - 2
	avail := w - 6 // borders (4) + selection prefix (2)

	// Keep the column cursor on screen by shifting whole columns.
	if g.colOff > len(g.Cols)-1 {
		g.colOff = clamp(len(g.Cols)-1, 0, len(g.Cols))
	}
	if g.ColCursor >= 0 {
		if g.ColCursor < g.colOff {
			g.colOff = g.ColCursor
		}
		for {
			idx, _ := g.visibleCols(avail)
			if len(idx) == 0 || g.ColCursor <= idx[len(idx)-1] {
				break
			}
			g.colOff++
		}
	}
	idx, widths := g.visibleCols(avail)

	// Keep the row cursor on screen.
	if g.Cursor >= 0 {
		if g.Cursor < g.rowOff {
			g.rowOff = g.Cursor
		}
		if g.Cursor >= g.rowOff+bodyH {
			g.rowOff = g.Cursor - bodyH + 1
		}
	}
	g.rowOff = clamp(g.rowOff, 0, max(0, len(g.Rows)-bodyH))

	// Overflow indicators: columns hidden left/right of the viewport, rows
	// below it.
	moreLeft := g.colOff > 0
	moreRight := len(idx) > 0 && idx[len(idx)-1] < len(g.Cols)-1
	moreDown := len(g.Rows) > g.rowOff+bodyH

	lines := make([]string, 0, h)
	lines = append(lines, g.headerBorder(w, idx, widths, focused, moreLeft, moreRight))
	side := S.Dim.Render("│")
	for i := 0; i < bodyH; i++ {
		ri := g.rowOff + i
		content := ""
		if ri < len(g.Rows) {
			content = g.renderRow(ri, idx, widths, avail, focused)
		}
		lines = append(lines, side+" "+padANSI(content, w-4)+" "+side)
	}
	bottom := []rune("└" + strings.Repeat("─", w-2) + "┘")
	if moreDown {
		bottom[len(bottom)-2] = '▾'
	}
	lines = append(lines, S.Dim.Render(string(bottom)))
	return lines
}

// headerBorder builds the top border with each header word overlaid on the
// dashes, aligned exactly above its column, one space of border around each
// word. Content offset j lands at absolute column j+2 (body rows are
// prefixed by "│ "), so a cell starting at content offset co places its
// header at canvas index co+2.
func (g *Grid) headerBorder(w int, idx, widths []int, focused, moreLeft, moreRight bool) string {
	canvas := make([]rune, w)
	for i := range canvas {
		canvas[i] = '─'
	}
	canvas[0] = '┌'
	canvas[w-1] = '┐'

	set := func(at int, r rune) {
		if at >= 1 && at <= w-2 {
			canvas[at] = r
		}
	}

	accentFrom, accentTo := -1, -1
	co := 2 // first cell content offset, after the 2-cell selection prefix
	for n, ci := range idx {
		word := truncTail(g.Cols[ci], widths[n])
		set(co+1, ' ') // space before the word
		at := co + 2
		for _, r := range word {
			rw := runewidth.RuneWidth(r)
			set(at, r)
			if rw == 2 {
				set(at+1, 0) // wide rune occupies the next cell too
			}
			at += rw
		}
		set(at, ' ') // space after the word
		if ci == g.ColCursor && focused {
			accentFrom, accentTo = co+2, at
		}
		co += widths[n] + 2
	}

	// Overflow chevrons win over anything a header word left there.
	if moreLeft {
		canvas[1] = '‹'
	}
	if moreRight {
		canvas[w-2] = '›'
	}

	join := func(runes []rune) string {
		var b strings.Builder
		for _, r := range runes {
			if r != 0 {
				b.WriteRune(r)
			}
		}
		return b.String()
	}
	if accentFrom < 0 {
		return S.Dim.Render(join(canvas))
	}
	return S.Dim.Render(join(canvas[:accentFrom])) +
		S.Accent.Render(join(canvas[accentFrom:accentTo])) +
		S.Dim.Render(join(canvas[accentTo:]))
}

// renderRow renders one data row (selection prefix + cells) at exactly
// avail+2 cells.
func (g *Grid) renderRow(ri int, idx, widths []int, avail int, focused bool) string {
	row := g.Rows[ri]
	selected := focused && ri == g.Cursor

	plain := make([]string, len(idx))
	for n, ci := range idx {
		text := ""
		if ci < len(row) {
			text = row[ci].Text
		}
		plain[n] = padRight(text, widths[n])
	}

	if selected {
		// Selected rows are monochrome: text on the selection background,
		// with the accent bar prefix.
		content := padRight(strings.Join(plain, "  "), avail)
		return S.SelBar.Render(glyphBar+" ") + S.SelText.Render(content)
	}

	parts := make([]string, len(idx))
	for n, ci := range idx {
		style := S.Text
		if ci < len(row) {
			switch row[ci].Style {
			case cellDim, cellNull:
				style = S.Dim
			case cellAccent:
				style = S.Accent
			}
		}
		parts[n] = style.Render(plain[n])
	}
	return "  " + strings.Join(parts, "  ")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
