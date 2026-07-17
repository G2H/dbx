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
	focusList = iota
	focusSchema
	focusPreview
)

type tablesLoadedMsg struct {
	token  int
	tables []engine.Table
	err    error
}

type tableInfoMsg struct {
	token      int
	table      engine.Table
	schema     []engine.ColumnInfo
	schemaErr  error
	common     map[string]string // column name -> "v1, v2, v3"
	preview    *query.Result
	previewErr error
}

type tablesView struct {
	app *App

	tables   []engine.Table
	loadErr  error
	filtered []int // indexes into tables
	cursor   int   // index into filtered
	listOff  int

	input     textinput.Model
	filtering bool

	focus int

	schema      []engine.ColumnInfo
	schemaErr   error
	preview     *query.Result
	previewErr  error
	schemaGrid  *Grid
	previewGrid *Grid
}

func newTablesView(a *App) *tablesView {
	ti := textinput.New()
	ti.Cursor.SetMode(cursor.CursorStatic)
	ti.Prompt = "/ "
	ti.PromptStyle = S.Accent
	ti.TextStyle = S.Text
	return &tablesView{app: a, input: ti}
}

func (v *tablesView) layout(inner int) {
	v.input.Width = max(4, v.leftWidth(inner)-8)
}

func (v *tablesView) leftWidth(inner int) int {
	lw := clamp(inner/4, 16, 36)
	if inner-lw-1 < 12 {
		lw = max(8, inner-13)
	}
	return lw
}

// load fetches the table list.
func (v *tablesView) load() tea.Cmd {
	eng := v.app.eng
	return v.app.startQuery(func(ctx context.Context, token int) tea.Msg {
		tables, err := eng.ListTables(ctx)
		return tablesLoadedMsg{token: token, tables: tables, err: err}
	})
}

func (v *tablesView) onTablesLoaded(msg tablesLoadedMsg) tea.Cmd {
	if !v.app.finishQuery(msg.token) {
		return nil
	}
	v.tables = msg.tables
	v.loadErr = msg.err
	v.applyFilter("")
	return v.loadInfo()
}

// loadInfo fetches schema + preview for the selected table.
func (v *tablesView) loadInfo() tea.Cmd {
	table, ok := v.selectedTable()
	if !ok {
		v.schema, v.preview = nil, nil
		v.schemaGrid, v.previewGrid = nil, nil
		return nil
	}
	eng := v.app.eng
	tableSQL := eng.QuoteTable(table)
	// DuckDB-backed engines get a fair random sample; SQLite has no native
	// sampling, so it keeps the cheap first-rows bound.
	mode := query.SampleReservoir
	if eng.Kind() == engine.KindSQLite {
		mode = query.SampleFirstRows
	}
	return v.app.startQuery(func(ctx context.Context, token int) tea.Msg {
		msg := tableInfoMsg{token: token, table: table}
		msg.schema, msg.schemaErr = eng.DescribeTable(ctx, table)
		msg.common = map[string]string{}
		for _, c := range msg.schema {
			counts, err := query.ValueCounts(ctx, eng.DB(), eng.QuoteIdent, tableSQL, c.Name, mode, query.SchemaSample, 3)
			if err != nil || len(counts) == 0 {
				continue
			}
			vals := make([]string, len(counts))
			for i, vc := range counts {
				vals[i] = vc.Value
				if vc.Null {
					vals[i] = glyphNull
				}
			}
			msg.common[c.Name] = strings.Join(vals, ", ")
		}
		// LIMIT MaxRows+1 so the scanner (capped at MaxRows) can tell
		// whether the table was truncated.
		q, args := query.BuildSelect(eng.QuoteIdent, tableSQL, nil, nil, nil, query.MaxRows+1)
		msg.preview, msg.previewErr = query.Run(ctx, eng.DB(), q, args...)
		return msg
	})
}

func (v *tablesView) onTableInfo(msg tableInfoMsg) {
	if !v.app.finishQuery(msg.token) {
		return
	}
	v.schema, v.schemaErr = msg.schema, msg.schemaErr
	v.preview, v.previewErr = msg.preview, msg.previewErr
	v.schemaGrid = newSchemaGrid(msg.schema, msg.common)
	v.schemaGrid.Cursor = 0
	if msg.preview != nil {
		v.previewGrid = GridFromResult(msg.preview)
		v.previewGrid.Cursor = 0
	} else {
		v.previewGrid = nil
	}
}

func newSchemaGrid(cols []engine.ColumnInfo, common map[string]string) *Grid {
	rows := make([][]GCell, len(cols))
	for i, c := range cols {
		null := "yes"
		if !c.Nullable {
			null = "no"
		}
		pk := GCell{Style: cellAccent}
		if c.PK {
			pk.Text = glyphBar
		}
		rows[i] = []GCell{
			{Text: c.Name},
			{Text: c.Type, Style: cellDim},
			{Text: null, Style: cellDim},
			pk,
			{Text: c.Default, Style: cellDim},
			{Text: common[c.Name], Style: cellDim},
		}
	}
	return NewGrid([]string{"column", "type", "null", "pk", "default", "common"}, rows)
}

// selectedTable returns the selected table and whether there is one.
func (v *tablesView) selectedTable() (engine.Table, bool) {
	if len(v.filtered) == 0 || v.cursor >= len(v.filtered) {
		return engine.Table{}, false
	}
	return v.tables[v.filtered[v.cursor]], true
}

// applyFilter recomputes the fuzzy-filtered list, keeping the selected
// table when it survives the filter.
func (v *tablesView) applyFilter(prevSelected string) {
	pattern := v.input.Value()
	v.filtered = v.filtered[:0]
	for i, t := range v.tables {
		if fuzzyMatch(pattern, t.Display()) {
			v.filtered = append(v.filtered, i)
		}
	}
	v.cursor = 0
	for fi, ti := range v.filtered {
		if v.tables[ti].Display() == prevSelected {
			v.cursor = fi
			break
		}
	}
}

func (v *tablesView) typing() bool { return v.filtering }

func (v *tablesView) typingKey(msg tea.KeyMsg) tea.Cmd {
	prev := v.selectedDisplay()
	switch msg.String() {
	case "esc":
		v.input.SetValue("")
		v.filtering = false
		v.input.Blur()
		v.applyFilter(prev)
	case "enter":
		v.filtering = false
		v.input.Blur()
	default:
		var cmd tea.Cmd
		v.input, cmd = v.input.Update(msg)
		v.applyFilter(prev)
		_ = cmd
	}
	if v.selectedDisplay() != prev {
		return v.loadInfo()
	}
	return nil
}

// selectedDisplay is the display name of the selection, "" when none.
func (v *tablesView) selectedDisplay() string {
	t, ok := v.selectedTable()
	if !ok {
		return ""
	}
	return t.Display()
}

func (v *tablesView) escKey() tea.Cmd {
	if v.input.Value() != "" {
		prev := v.selectedDisplay()
		v.input.SetValue("")
		v.applyFilter(prev)
		if v.selectedDisplay() != prev {
			return v.loadInfo()
		}
	}
	return nil
}

func (v *tablesView) key(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "tab":
		v.focus = (v.focus + 1) % 3
		return nil
	case "shift+tab":
		v.focus = (v.focus + 2) % 3
		return nil
	}

	switch v.focus {
	case focusList:
		return v.listKey(msg)
	case focusSchema:
		if v.schemaGrid != nil {
			gridMoveKey(v.schemaGrid, msg)
		}
	case focusPreview:
		if v.previewGrid != nil {
			gridMoveKey(v.previewGrid, msg)
		}
	}
	return nil
}

// gridMoveKey applies shared movement keys to a grid.
func gridMoveKey(g *Grid, msg tea.KeyMsg) {
	switch msg.String() {
	case "up", "k":
		g.Move(-1)
	case "down", "j":
		g.Move(1)
	case "pgup":
		g.Move(-10)
	case "pgdown":
		g.Move(10)
	case "left", "h":
		g.MoveCol(-1)
	case "right", "l":
		g.MoveCol(1)
	}
}

func (v *tablesView) listKey(msg tea.KeyMsg) tea.Cmd {
	prev := v.selectedDisplay()
	switch msg.String() {
	case "up", "k":
		v.cursor = clamp(v.cursor-1, 0, max(0, len(v.filtered)-1))
	case "down", "j":
		v.cursor = clamp(v.cursor+1, 0, max(0, len(v.filtered)-1))
	case "pgup":
		v.cursor = clamp(v.cursor-10, 0, max(0, len(v.filtered)-1))
	case "pgdown":
		v.cursor = clamp(v.cursor+10, 0, max(0, len(v.filtered)-1))
	case "enter":
		if t, ok := v.selectedTable(); ok {
			return v.app.openDetails(t)
		}
	case "/":
		v.filtering = true
		v.input.Focus()
	}
	if v.selectedDisplay() != prev {
		return v.loadInfo()
	}
	return nil
}

func (v *tablesView) render(w, h int) []string {
	leftW := v.leftWidth(w)
	rightW := w - leftW - 1

	left := v.renderList(leftW, h)

	schemaH := clamp(len(v.schema)+2, 4, h/2)
	if v.schemaGrid == nil {
		schemaH = h / 2
	}
	previewH := h - schemaH

	var right []string
	right = append(right, v.renderSchema(rightW, schemaH)...)
	right = append(right, v.renderPreview(rightW, previewH)...)

	return hjoin(left, right)
}

func (v *tablesView) renderList(w, h int) []string {
	title := fmt.Sprintf("tables (%d)", len(v.tables))
	if v.input.Value() != "" || v.filtering {
		title = fmt.Sprintf("tables (%d/%d)", len(v.filtered), len(v.tables))
	}

	var body []string
	if v.loadErr != nil {
		body = append(body, S.Fail.Render(truncTail(glyphFail+" "+oneLine(v.loadErr.Error()), w-4)))
		return renderBox(title, v.focus == focusList, w, h, body)
	}
	showInput := v.filtering || v.input.Value() != ""
	if showInput {
		body = append(body, v.input.View())
	}

	itemH := h - 2 - len(body)
	if v.cursor < v.listOff {
		v.listOff = v.cursor
	}
	if v.cursor >= v.listOff+itemH {
		v.listOff = v.cursor - itemH + 1
	}
	v.listOff = clamp(v.listOff, 0, max(0, len(v.filtered)-itemH))

	nameW := w - 6 // box frame (4) + selection prefix (2)
	for i := v.listOff; i < len(v.filtered) && i-v.listOff < itemH; i++ {
		t := v.tables[v.filtered[i]]
		suffix := ""
		if t.IsView {
			suffix = " · view"
		}
		name := truncMiddle(t.Display(), nameW-dw(suffix))
		if i == v.cursor && v.focus == focusList {
			line := S.SelBar.Render(glyphBar+" ") + S.SelText.Render(padRight(name+suffix, nameW))
			body = append(body, line)
		} else {
			body = append(body, "  "+S.Text.Render(name)+S.Dim.Render(suffix))
		}
	}
	return renderBox(title, v.focus == focusList, w, h, body)
}

func (v *tablesView) renderSchema(w, h int) []string {
	if v.schemaErr != nil {
		return renderBox("schema", v.focus == focusSchema, w, h,
			[]string{S.Fail.Render(truncTail(glyphWarn+" "+oneLine(v.schemaErr.Error()), w-4))})
	}
	if v.schemaGrid == nil {
		return renderBox("schema", v.focus == focusSchema, w, h, nil)
	}
	return v.schemaGrid.Render(w, h, v.focus == focusSchema)
}

func (v *tablesView) renderPreview(w, h int) []string {
	if v.previewErr != nil {
		return renderBox("preview", v.focus == focusPreview, w, h,
			[]string{S.Fail.Render(truncTail(glyphFail+" "+oneLine(v.previewErr.Error()), w-4))})
	}
	if v.previewGrid == nil {
		return renderBox("preview", v.focus == focusPreview, w, h, nil)
	}
	return v.previewGrid.Render(w, h, v.focus == focusPreview)
}

func (v *tablesView) status() string {
	if v.loadErr != nil {
		return S.Fail.Render(glyphFail + " " + oneLine(v.loadErr.Error()))
	}
	if v.previewErr != nil {
		return queryStatus(0, 0, false, v.previewErr)
	}
	if v.preview != nil {
		return queryStatus(len(v.preview.Rows), v.preview.Elapsed, v.preview.Truncated, nil)
	}
	return ""
}

func (v *tablesView) hints() []hint {
	if v.filtering {
		return []hint{{"↵", "apply"}, {"esc", "clear"}}
	}
	switch v.focus {
	case focusSchema:
		return []hint{{"↑↓", "scroll"}, {"tab", "focus"}, {"s", "sql"}, {"q", "quit"}}
	case focusPreview:
		return []hint{{"↑↓", "scroll"}, {"←→", "columns"}, {"tab", "focus"}, {"s", "sql"}, {"q", "quit"}}
	default:
		hints := []hint{{"↑↓", "select"}, {"↵", "details"}, {"tab", "focus"}, {"/", "filter"}, {"s", "sql"}, {"q", "quit"}}
		if v.input.Value() != "" {
			hints = append(hints[:4:4], hint{"esc", "clear filter"}, hints[4], hints[5])
		}
		return hints
	}
}
