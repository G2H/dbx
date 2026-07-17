package ui

import (
	"strings"
	"testing"
)

func plain(s string) string { return stripToWidth(s) }

// colOf returns the display column at which sub starts within s, or -1.
func colOf(s, sub string) int {
	i := strings.Index(s, sub)
	if i < 0 {
		return -1
	}
	return dw(s[:i])
}

func testGrid() *Grid {
	return NewGrid(
		[]string{"id", "name", "email"},
		[][]GCell{
			{{Text: "1"}, {Text: "Ada Lovelace"}, {Text: "ada@example.com"}},
			{{Text: "2"}, {Text: "Alan Turing"}, {Text: glyphNull, Style: cellNull}},
		},
	)
}

func TestGridHeaderAlignsWithCells(t *testing.T) {
	g := testGrid()
	lines := g.Render(60, 6, false)
	if len(lines) != 6 {
		t.Fatalf("got %d lines, want 6", len(lines))
	}
	header := plain(lines[0])
	firstRow := plain(lines[1])

	for _, col := range []string{"id", "name", "email"} {
		if !strings.Contains(header, col) {
			t.Fatalf("header %q missing column %q", header, col)
		}
	}
	// Header words align exactly above the cell text (display columns,
	// not byte offsets: the border characters are multi-byte).
	if colOf(header, "id") != colOf(firstRow, "1") {
		t.Fatalf("id header at col %d but cell at col %d\n%s\n%s",
			colOf(header, "id"), colOf(firstRow, "1"), header, firstRow)
	}
	if colOf(header, "name") != colOf(firstRow, "Ada") {
		t.Fatalf("name header misaligned\n%s\n%s", header, firstRow)
	}
	// One space of border around each header word.
	if !strings.Contains(header, " id ") || !strings.Contains(header, " name ") {
		t.Fatalf("header words not surrounded by spaces: %q", header)
	}
	// Frame corners intact.
	if !strings.HasPrefix(header, "┌") || !strings.HasSuffix(header, "┐") {
		t.Fatalf("bad frame: %q", header)
	}
	// Every line is exactly 60 cells wide.
	for i, l := range lines {
		if got := dw(plain(l)); got != 60 {
			t.Fatalf("line %d is %d cells wide, want 60: %q", i, got, plain(l))
		}
	}
}

func TestGridNullRendering(t *testing.T) {
	g := testGrid()
	lines := g.Render(60, 6, false)
	if !strings.Contains(plain(lines[2]), glyphNull) {
		t.Fatalf("NULL cell should render as %s: %q", glyphNull, plain(lines[2]))
	}
}

func TestGridSelectionBar(t *testing.T) {
	g := testGrid()
	g.Cursor = 1
	lines := g.Render(60, 6, true)
	if !strings.Contains(plain(lines[2]), glyphBar) {
		t.Fatalf("selected row should carry the %s bar: %q", glyphBar, plain(lines[2]))
	}
	// Unselected rows keep the two-space prefix so columns align.
	if !strings.HasPrefix(plain(lines[1]), "│   ") {
		t.Fatalf("unselected row should be prefixed with spaces: %q", plain(lines[1]))
	}
}

func TestGridHorizontalScroll(t *testing.T) {
	cols := []string{"aaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbb", "cccccccccccccccccccc", "dddddddddddddddddddd"}
	row := make([]GCell, len(cols))
	for i := range row {
		row[i] = GCell{Text: "x"}
	}
	g := NewGrid(cols, [][]GCell{row})
	g.ColCursor = 0

	lines := g.Render(50, 4, true)
	if !strings.Contains(plain(lines[0]), "aaaa") {
		t.Fatalf("first column should be visible: %q", plain(lines[0]))
	}
	if strings.Contains(plain(lines[0]), "dddd") {
		t.Fatalf("last column should not fit at width 50: %q", plain(lines[0]))
	}

	// Moving the column cursor to the last column shifts whole columns.
	g.MoveCol(3)
	lines = g.Render(50, 4, true)
	if !strings.Contains(plain(lines[0]), "dddd") {
		t.Fatalf("after scrolling, last column should be visible: %q", plain(lines[0]))
	}
	if strings.Contains(plain(lines[0]), "aaaa") {
		t.Fatalf("after scrolling, first column should be gone: %q", plain(lines[0]))
	}
}

func TestGridVerticalScrollFollowsCursor(t *testing.T) {
	rows := make([][]GCell, 50)
	for i := range rows {
		rows[i] = []GCell{{Text: strings.Repeat("r", 1) + string(rune('0'+i%10))}}
	}
	g := NewGrid([]string{"col"}, rows)
	g.Cursor = 0
	g.Render(20, 7, true) // 5 body rows
	g.Move(30)
	lines := g.Render(20, 7, true)
	found := false
	for _, l := range lines {
		if strings.Contains(plain(l), glyphBar) {
			found = true
		}
	}
	if !found {
		t.Fatal("cursor row must stay visible after moving beyond the viewport")
	}
}

func TestGridClampsSingleWideColumn(t *testing.T) {
	g := NewGrid([]string{"c"}, [][]GCell{{{Text: strings.Repeat("x", 100)}}})
	lines := g.Render(30, 4, false)
	for i, l := range lines {
		if got := dw(plain(l)); got != 30 {
			t.Fatalf("line %d is %d wide, want 30", i, got)
		}
	}
}

func TestRenderBoxTitleAndWidth(t *testing.T) {
	lines := renderBox("tables (12)", false, 40, 5, []string{"hello"})
	if len(lines) != 5 {
		t.Fatalf("got %d lines, want 5", len(lines))
	}
	if !strings.HasPrefix(plain(lines[0]), "┌─ tables (12) ─") {
		t.Fatalf("bad top border: %q", plain(lines[0]))
	}
	for i, l := range lines {
		if got := dw(plain(l)); got != 40 {
			t.Fatalf("line %d is %d wide, want 40: %q", i, got, plain(l))
		}
	}
	if !strings.Contains(plain(lines[1]), "hello") {
		t.Fatalf("body missing: %q", plain(lines[1]))
	}
}
