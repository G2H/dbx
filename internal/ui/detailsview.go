package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"dbx/internal/engine"
	"dbx/internal/query"
)

const (
	panelNone = iota
	panelColumns
	panelFilters
	panelValues // common values of one column, selectable as a filter
)

// valuePickerK is how many common values the filter sidebar offers.
const valuePickerK = 30

type detailsLoadedMsg struct {
	token    int
	table    engine.Table
	rowsOnly bool
	cols     []engine.ColumnInfo
	colsErr  error
	res      *query.Result
	err      error
}

type valuesLoadedMsg struct {
	token  int
	table  engine.Table
	col    string
	values []query.ValueCount
	err    error
}

type detailsView struct {
	app *App

	table    engine.Table
	hasTable bool
	cols     []engine.ColumnInfo
	colsErr  error
	visible  []bool // aligned to cols

	filters []query.Filter
	order   *query.Order

	res  *query.Result
	err  error
	grid *Grid

	colCursor int

	panel       int
	panelCursor int

	// panelValues state
	valueCol   string
	values     []query.ValueCount
	valuesSel  []bool
	valuesErr  error
	valuesLoad bool

	input     textinput.Model
	filtering bool // the raw where input is open
	editIdx   int  // filter index being edited from the filters panel, -1 = new
}

func newDetailsView(a *App) *detailsView {
	ti := textinput.New()
	ti.Cursor.SetMode(cursor.CursorStatic)
	ti.Prompt = ""
	ti.TextStyle = S.Text
	ti.PlaceholderStyle = S.Dim
	return &detailsView{app: a, input: ti, editIdx: -1}
}

func (v *detailsView) layout(inner int) {
	v.input.Width = max(8, inner-30)
}

// open shows the details view for a table, resetting state when the table
// changed, and loads schema + rows.
func (v *detailsView) open(table engine.Table) tea.Cmd {
	if table != v.table || !v.hasTable {
		v.table = table
		v.hasTable = true
		v.cols, v.colsErr = nil, nil
		v.visible = nil
		v.filters = nil
		v.order = nil
		v.res, v.err, v.grid = nil, nil, nil
		v.colCursor = 0
		v.panel = panelNone
		v.filtering = false
	}
	return v.load(false)
}

// visibleColNames returns the names of the columns currently shown.
func (v *detailsView) visibleColNames() []string {
	var names []string
	for i, c := range v.cols {
		if v.visible[i] {
			names = append(names, c.Name)
		}
	}
	return names
}

// load fetches rows (and the schema too unless rowsOnly).
func (v *detailsView) load(rowsOnly bool) tea.Cmd {
	eng := v.app.eng
	table := v.table
	filters := append([]query.Filter(nil), v.filters...)
	order := v.order
	var sel []string
	if rowsOnly {
		sel = v.visibleColNames()
		if len(sel) == 0 {
			// Nothing selected to show; skip the query entirely.
			v.res, v.err, v.grid = nil, nil, nil
			return nil
		}
	}
	tableSQL := eng.QuoteTable(table)
	return v.app.startQuery(func(ctx context.Context, token int) tea.Msg {
		msg := detailsLoadedMsg{token: token, table: table, rowsOnly: rowsOnly}
		if !rowsOnly {
			msg.cols, msg.colsErr = eng.DescribeTable(ctx, table)
		}
		// MaxRows+1 lets the scanner detect truncation (it caps at MaxRows).
		q, args := query.BuildSelect(eng.QuoteIdent, tableSQL, sel, filters, order, query.MaxRows+1)
		msg.res, msg.err = query.Run(ctx, eng.DB(), q, args...)
		return msg
	})
}

func (v *detailsView) onLoaded(msg detailsLoadedMsg) {
	if !v.app.finishQuery(msg.token) || msg.table != v.table {
		return
	}
	if !msg.rowsOnly {
		v.cols, v.colsErr = msg.cols, msg.colsErr
		v.visible = make([]bool, len(v.cols))
		for i := range v.visible {
			v.visible[i] = true
		}
	}
	v.res, v.err = msg.res, msg.err
	if msg.res != nil {
		v.grid = GridFromResult(msg.res)
		v.grid.Cursor = 0
		if len(v.grid.Cols) > 0 {
			v.grid.ColCursor = clamp(v.colCursor, 0, len(v.grid.Cols)-1)
			v.colCursor = v.grid.ColCursor
		}
	} else {
		v.grid = nil
	}
}

// openValuesPanel shows the common-values sidebar for a column.
func (v *detailsView) openValuesPanel(col string) tea.Cmd {
	if col == "" {
		return nil
	}
	v.panel = panelValues
	v.panelCursor = 0
	v.valueCol = col
	v.values, v.valuesSel, v.valuesErr = nil, nil, nil
	v.valuesLoad = true
	eng := v.app.eng
	table := v.table
	tableSQL := eng.QuoteTable(table)
	return v.app.startQuery(func(ctx context.Context, token int) tea.Msg {
		values, err := query.ValueCounts(ctx, eng.DB(), eng.QuoteIdent, tableSQL, col, query.SampleNone, 0, valuePickerK)
		return valuesLoadedMsg{token: token, table: table, col: col, values: values, err: err}
	})
}

func (v *detailsView) onValuesLoaded(msg valuesLoadedMsg) {
	if !v.app.finishQuery(msg.token) || msg.table != v.table || msg.col != v.valueCol {
		return
	}
	v.valuesLoad = false
	v.values, v.valuesErr = msg.values, msg.err
	v.valuesSel = make([]bool, len(v.values))
	// Preselect the values already filtered on.
	for _, f := range v.filters {
		if f.Column != msg.col || f.Op != query.OpIn {
			continue
		}
		for i, vc := range v.values {
			if vc.Null {
				v.valuesSel[i] = f.IncludeNull
				continue
			}
			for _, sel := range f.Values {
				if sel == vc.Value {
					v.valuesSel[i] = true
				}
			}
		}
	}
}

// applyValueSelection rebuilds the OpIn filter for valueCol from the
// checkbox state (removing it when nothing is selected) and reloads rows.
func (v *detailsView) applyValueSelection() tea.Cmd {
	f := query.Filter{Column: v.valueCol, Op: query.OpIn}
	for i, vc := range v.values {
		if !v.valuesSel[i] {
			continue
		}
		if vc.Null {
			f.IncludeNull = true
		} else {
			f.Values = append(f.Values, vc.Value)
		}
	}
	v.removeColumnFilter(v.valueCol)
	if len(f.Values) > 0 || f.IncludeNull {
		v.filters = append(v.filters, f)
	}
	return v.load(true)
}

// removeColumnFilter drops any non-raw filter on col.
func (v *detailsView) removeColumnFilter(col string) {
	kept := v.filters[:0]
	for _, f := range v.filters {
		if f.Op == query.OpRaw || f.Column != col {
			kept = append(kept, f)
		}
	}
	v.filters = kept
}

// cycleOrder steps the sort on col: none -> ascending -> descending -> none.
func (v *detailsView) cycleOrder(col string) tea.Cmd {
	if col == "" {
		return nil
	}
	switch {
	case v.order == nil || v.order.Column != col:
		v.order = &query.Order{Column: col}
	case !v.order.Desc:
		v.order.Desc = true
	default:
		v.order = nil
	}
	return v.load(true)
}

// currentColumn is the name of the column under the column cursor.
func (v *detailsView) currentColumn() string {
	if v.grid == nil || len(v.grid.Cols) == 0 {
		return ""
	}
	return v.grid.Cols[clamp(v.grid.ColCursor, 0, len(v.grid.Cols)-1)]
}

func (v *detailsView) typing() bool { return v.filtering }

func (v *detailsView) typingKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		v.filtering = false
		v.input.Blur()
		return nil
	case "enter":
		v.filtering = false
		v.input.Blur()
		return v.applyWhereInput(v.input.Value())
	default:
		var cmd tea.Cmd
		v.input, cmd = v.input.Update(msg)
		_ = cmd
		return nil
	}
}

// applyWhereInput adds or edits a raw SQL filter, then reloads rows.
func (v *detailsView) applyWhereInput(text string) tea.Cmd {
	text = strings.TrimSpace(text)
	if text == "" {
		if v.editIdx >= 0 && v.editIdx < len(v.filters) {
			v.filters = append(v.filters[:v.editIdx], v.filters[v.editIdx+1:]...)
			v.editIdx = -1
			return v.load(true)
		}
		return nil
	}
	f := query.Filter{Op: query.OpRaw, Value: text}
	if v.editIdx >= 0 && v.editIdx < len(v.filters) {
		v.filters[v.editIdx] = f
	} else {
		v.filters = append(v.filters, f)
	}
	v.editIdx = -1
	return v.load(true)
}

// startWhereInput opens the raw SQL input, optionally editing an existing
// raw filter (editIdx >= 0).
func (v *detailsView) startWhereInput(editIdx int) {
	v.editIdx = editIdx
	v.input.Placeholder = "sql, e.g. age > 30 and name like 'A%'"
	v.input.SetValue("")
	if editIdx >= 0 && editIdx < len(v.filters) {
		v.input.SetValue(v.filters[editIdx].Value)
	}
	v.filtering = true
	v.input.Focus()
	v.input.CursorEnd()
}

func (v *detailsView) escKey() tea.Cmd {
	if v.panel != panelNone {
		v.panel = panelNone
		return nil
	}
	v.app.view = viewTables
	return nil
}

func (v *detailsView) key(msg tea.KeyMsg) tea.Cmd {
	switch v.panel {
	case panelColumns:
		return v.columnsPanelKey(msg)
	case panelFilters:
		return v.filtersPanelKey(msg)
	case panelValues:
		return v.valuesPanelKey(msg)
	}

	switch msg.String() {
	case "c":
		v.panel = panelColumns
		v.panelCursor = 0
		return nil
	case "f":
		return v.openValuesPanel(v.currentColumn())
	case "w":
		v.startWhereInput(-1)
		return nil
	case "o":
		return v.cycleOrder(v.currentColumn())
	case "F":
		if len(v.filters) > 0 {
			v.panel = panelFilters
			v.panelCursor = 0
		}
		return nil
	case "ctrl+r":
		if len(v.filters) > 0 {
			prompt := fmt.Sprintf("clear all %d filters?", len(v.filters))
			if len(v.filters) == 1 {
				prompt = "clear the filter?"
			}
			v.app.confirm = &confirmState{
				prompt: prompt,
				yes: func(a *App) tea.Cmd {
					a.dv.filters = nil
					return a.dv.load(true)
				},
			}
		}
		return nil
	}
	if v.grid != nil {
		gridMoveKey(v.grid, msg)
		v.colCursor = v.grid.ColCursor
	}
	return nil
}

func (v *detailsView) columnsPanelKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "c":
		v.panel = panelNone
	case "up", "k":
		v.panelCursor = clamp(v.panelCursor-1, 0, max(0, len(v.cols)-1))
	case "down", "j":
		v.panelCursor = clamp(v.panelCursor+1, 0, max(0, len(v.cols)-1))
	case " ":
		if v.panelCursor < len(v.visible) {
			v.visible[v.panelCursor] = !v.visible[v.panelCursor]
			return v.load(true)
		}
	case "a":
		all := true
		for _, on := range v.visible {
			if !on {
				all = false
				break
			}
		}
		for i := range v.visible {
			v.visible[i] = !all
		}
		return v.load(true)
	}
	return nil
}

func (v *detailsView) valuesPanelKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "f":
		v.panel = panelNone
	case "up", "k":
		v.panelCursor = clamp(v.panelCursor-1, 0, max(0, len(v.values)-1))
	case "down", "j":
		v.panelCursor = clamp(v.panelCursor+1, 0, max(0, len(v.values)-1))
	case " ", "enter":
		if v.panelCursor < len(v.valuesSel) {
			v.valuesSel[v.panelCursor] = !v.valuesSel[v.panelCursor]
			return v.applyValueSelection()
		}
	}
	return nil
}

func (v *detailsView) filtersPanelKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "F":
		v.panel = panelNone
	case "up", "k":
		v.panelCursor = clamp(v.panelCursor-1, 0, max(0, len(v.filters)-1))
	case "down", "j":
		v.panelCursor = clamp(v.panelCursor+1, 0, max(0, len(v.filters)-1))
	case "enter":
		if v.panelCursor < len(v.filters) {
			f := v.filters[v.panelCursor]
			idx := v.panelCursor
			v.panel = panelNone
			if f.Op == query.OpIn {
				return v.openValuesPanel(f.Column)
			}
			v.startWhereInput(idx)
		}
	case "x":
		if v.panelCursor < len(v.filters) {
			v.filters = append(v.filters[:v.panelCursor], v.filters[v.panelCursor+1:]...)
			v.panelCursor = clamp(v.panelCursor, 0, max(0, len(v.filters)-1))
			if len(v.filters) == 0 {
				v.panel = panelNone
			}
			return v.load(true)
		}
	}
	return nil
}

func (v *detailsView) render(w, h int) []string {
	gridW := w
	var panel []string
	if v.panel != panelNone {
		panelW := clamp(w/3, 20, 36)
		gridW = w - panelW - 1
		switch v.panel {
		case panelColumns:
			panel = v.renderColumnsPanel(panelW, h)
		case panelFilters:
			panel = v.renderFiltersPanel(panelW, h)
		case panelValues:
			panel = v.renderValuesPanel(panelW, h)
		}
	}

	var grid []string
	focused := v.panel == panelNone && !v.filtering
	switch {
	case v.err != nil:
		grid = renderBox(v.table.Display(), focused, gridW, h,
			wrapStyled(glyphFail+" "+oneLine(v.err.Error()), gridW-4, S.Fail))
	case v.grid != nil:
		grid = v.grid.Render(gridW, h, focused)
	default:
		note := "no visible columns · c to choose"
		grid = renderBox(v.table.Display(), focused, gridW, h, []string{S.Dim.Render(note)})
	}

	if panel == nil {
		return grid
	}
	return hjoin(grid, panel)
}

// wrapStyled hard-wraps text to width and styles each line.
func wrapStyled(text string, width int, style interface{ Render(...string) string }) []string {
	if width < 4 {
		width = 4
	}
	var lines []string
	for len(text) > 0 && len(lines) < 20 {
		line := truncTail(text, width)
		if dw(text) <= width {
			line = text
			text = ""
		} else {
			// Cut without the ellipsis for continuation.
			runes := []rune(text)
			w := 0
			i := 0
			for ; i < len(runes); i++ {
				w += dw(string(runes[i]))
				if w > width {
					break
				}
			}
			line = string(runes[:i])
			text = string(runes[i:])
		}
		lines = append(lines, style.Render(line))
	}
	return lines
}

// panelWindow returns the first visible item index for a scrolling panel.
func panelWindow(cursor, count, height int) int {
	return clamp(cursor-(height-1), 0, max(0, count-height))
}

func (v *detailsView) renderColumnsPanel(w, h int) []string {
	body := make([]string, 0, len(v.cols))
	itemW := w - 6
	start := panelWindow(v.panelCursor, len(v.cols), h-2)
	for i := start; i < len(v.cols) && len(body) < h-2; i++ {
		mark := "[ ]"
		if v.visible[i] {
			mark = "[x]"
		}
		text := mark + " " + truncMiddle(v.cols[i].Name, itemW-4)
		if i == v.panelCursor {
			body = append(body, S.SelBar.Render(glyphBar+" ")+S.SelText.Render(padRight(text, itemW)))
		} else {
			body = append(body, "  "+S.Text.Render(text))
		}
	}
	return renderBox("columns", true, w, h, body)
}

func (v *detailsView) renderValuesPanel(w, h int) []string {
	title := "filter " + v.valueCol
	itemW := w - 6
	var body []string
	switch {
	case v.valuesLoad:
		body = append(body, S.Dim.Render("loading values…"))
	case v.valuesErr != nil:
		body = append(body, S.Fail.Render(truncTail(glyphWarn+" "+oneLine(v.valuesErr.Error()), itemW+2)))
	case len(v.values) == 0:
		body = append(body, S.Dim.Render("no values"))
	default:
		start := panelWindow(v.panelCursor, len(v.values), h-2)
		for i := start; i < len(v.values) && len(body) < h-2; i++ {
			vc := v.values[i]
			mark := "[ ]"
			if v.valuesSel[i] {
				mark = "[x]"
			}
			label := vc.Value
			if vc.Null {
				label = glyphNull
			}
			count := fmt.Sprintf(" ×%d", vc.Count)
			label = truncTail(label, itemW-4-dw(count))
			if i == v.panelCursor {
				text := mark + " " + label + count
				body = append(body, S.SelBar.Render(glyphBar+" ")+S.SelText.Render(padRight(text, itemW)))
			} else {
				body = append(body, "  "+S.Text.Render(mark+" "+label)+S.Dim.Render(count))
			}
		}
	}
	return renderBox(title, true, w, h, body)
}

func (v *detailsView) renderFiltersPanel(w, h int) []string {
	body := make([]string, 0, len(v.filters))
	itemW := w - 6
	for i, f := range v.filters {
		text := truncTail(f.Describe(), itemW)
		if i == v.panelCursor {
			body = append(body, S.SelBar.Render(glyphBar+" ")+S.SelText.Render(padRight(text, itemW)))
		} else {
			body = append(body, "  "+S.Text.Render(text))
		}
	}
	return renderBox(fmt.Sprintf("filters (%d)", len(v.filters)), true, w, h, body)
}

// filterSummary renders `2 filters · name ~ ada · age > 30` (dim).
func (v *detailsView) filterSummary() string {
	if len(v.filters) == 0 {
		return ""
	}
	noun := "filters"
	if len(v.filters) == 1 {
		noun = "filter"
	}
	parts := []string{fmt.Sprintf("%d %s", len(v.filters), noun)}
	for _, f := range v.filters {
		parts = append(parts, f.Describe())
	}
	return S.Dim.Render(strings.Join(parts, " · "))
}

func (v *detailsView) status() string {
	if v.filtering {
		return S.Accent.Render(glyphBar+" where ") + v.input.View()
	}
	var s string
	switch {
	case v.err != nil:
		s = queryStatus(0, 0, false, v.err)
	case v.res != nil:
		s = queryStatus(len(v.res.Rows), v.res.Elapsed, v.res.Truncated, nil)
	}
	if fs := v.filterSummary(); fs != "" {
		if s != "" {
			s += S.Dim.Render(" · ") + fs
		} else {
			s = fs
		}
	}
	if v.order != nil {
		os := S.Dim.Render(v.order.Describe())
		if s != "" {
			s += S.Dim.Render(" · ") + os
		} else {
			s = os
		}
	}
	if v.colsErr != nil {
		warn := S.Fail.Render(glyphWarn + " " + oneLine(v.colsErr.Error()))
		if s != "" {
			s = warn + S.Dim.Render(" · ") + s
		} else {
			s = warn
		}
	}
	return s
}

func (v *detailsView) hints() []hint {
	if v.filtering {
		return []hint{{"↵", "apply"}, {"esc", "cancel"}}
	}
	switch v.panel {
	case panelColumns:
		return []hint{{"space", "toggle"}, {"a", "all"}, {"↑↓", "select"}, {"esc", "close"}}
	case panelValues:
		return []hint{{"space", "toggle"}, {"↑↓", "select"}, {"esc", "close"}}
	case panelFilters:
		return []hint{{"↵", "edit"}, {"x", "remove"}, {"↑↓", "select"}, {"esc", "close"}}
	}
	hints := []hint{{"↑↓ ←→", "move"}, {"c", "columns"}, {"f", "filter"}, {"w", "where"}, {"o", "sort"}}
	if len(v.filters) > 0 {
		hints = append(hints, hint{"F", "filters"}, hint{"ctrl+r", "clear all"})
	}
	hints = append(hints, hint{"esc", "back"}, hint{"s", "sql"}, hint{"q", "quit"})
	return hints
}
