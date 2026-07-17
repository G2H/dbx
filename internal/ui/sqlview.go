package ui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"dbx/internal/query"
)

type sqlResultMsg struct {
	token int
	res   *query.Result
	err   error
}

type sqlView struct {
	app *App

	ta      textarea.Model
	editing bool // editor focused

	res  *query.Result
	err  error
	grid *Grid
	ran  bool // at least one query executed

	history []string
	histIdx int    // == len(history) means "live draft"
	draft   string // editor content saved while browsing history
}

func newSQLView(a *App) *sqlView {
	ta := textarea.New()
	ta.Cursor.SetMode(cursor.CursorStatic)
	ta.Placeholder = "select * from ..."
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	// Match the design language: no painted cursor-line background, dim
	// placeholder, plain text.
	ta.FocusedStyle.CursorLine = S.Text
	ta.FocusedStyle.Placeholder = S.Dim
	ta.FocusedStyle.Text = S.Text
	ta.BlurredStyle.Placeholder = S.Dim
	ta.BlurredStyle.Text = S.Text
	v := &sqlView{app: a, ta: ta, editing: true}
	v.ta.Focus()
	return v
}

func (v *sqlView) focusEditor() {
	v.editing = true
	v.ta.Focus()
}

func (v *sqlView) layout(inner, bodyH int) {
	v.ta.SetWidth(inner - 4)
	v.ta.SetHeight(max(1, v.editorHeight(bodyH)-2))
}

func (v *sqlView) editorHeight(bodyH int) int {
	return clamp(bodyH/3, 5, 10)
}

func (v *sqlView) run() tea.Cmd {
	sqlText := strings.TrimSpace(v.ta.Value())
	if sqlText == "" {
		return nil
	}
	if len(v.history) == 0 || v.history[len(v.history)-1] != sqlText {
		v.history = append(v.history, sqlText)
	}
	v.histIdx = len(v.history)
	v.draft = ""
	db := v.app.eng.DB()
	return v.app.startQuery(func(ctx context.Context, token int) tea.Msg {
		res, err := query.Run(ctx, db, sqlText)
		return sqlResultMsg{token: token, res: res, err: err}
	})
}

func (v *sqlView) onResult(msg sqlResultMsg) {
	if !v.app.finishQuery(msg.token) {
		return
	}
	v.ran = true
	v.res, v.err = msg.res, msg.err
	if msg.res != nil {
		v.grid = GridFromResult(msg.res)
		v.grid.Cursor = 0
	} else {
		v.grid = nil
	}
}

func (v *sqlView) typing() bool { return v.editing }

func (v *sqlView) typingKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		v.editing = false
		v.ta.Blur()
		return nil
	case "tab":
		v.editing = false
		v.ta.Blur()
		return nil
	case "ctrl+enter", "alt+enter", "ctrl+r", "f5":
		return v.run()
	case "ctrl+p":
		if v.histIdx > 0 {
			if v.histIdx == len(v.history) {
				v.draft = v.ta.Value()
			}
			v.histIdx--
			v.ta.SetValue(v.history[v.histIdx])
		}
		return nil
	case "ctrl+n":
		if v.histIdx < len(v.history) {
			v.histIdx++
			if v.histIdx == len(v.history) {
				v.ta.SetValue(v.draft)
			} else {
				v.ta.SetValue(v.history[v.histIdx])
			}
		}
		return nil
	}
	var cmd tea.Cmd
	v.ta, cmd = v.ta.Update(msg)
	return cmd
}

// escKey (editor already blurred) flips back to the table screen.
func (v *sqlView) escKey() tea.Cmd {
	v.app.view = v.app.lastTable
	return nil
}

func (v *sqlView) key(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "tab", "enter":
		v.focusEditor()
		return nil
	case "alt+enter", "ctrl+r", "f5":
		return v.run()
	}
	if v.grid != nil {
		gridMoveKey(v.grid, msg)
	}
	return nil
}

func (v *sqlView) render(w, h int) []string {
	edH := v.editorHeight(h)
	resH := h - edH

	editor := renderBox("sql", v.editing, w, edH, strings.Split(v.ta.View(), "\n"))

	var results []string
	switch {
	case v.err != nil:
		results = renderBox("results", !v.editing, w, resH,
			wrapStyled(glyphFail+" "+oneLine(v.err.Error()), w-4, S.Fail))
	case v.grid != nil:
		results = v.grid.Render(w, resH, !v.editing)
	default:
		results = renderBox("results", !v.editing, w, resH,
			[]string{S.Dim.Render("alt+↵ or F5 runs the query")})
	}
	return append(editor, results...)
}

func (v *sqlView) status() string {
	if !v.ran {
		return ""
	}
	if v.err != nil {
		return queryStatus(0, 0, false, v.err)
	}
	if v.res != nil {
		return queryStatus(len(v.res.Rows), v.res.Elapsed, v.res.Truncated, nil)
	}
	return ""
}

func (v *sqlView) hints() []hint {
	if v.editing {
		return []hint{{"alt+↵", "run"}, {"ctrl+p ctrl+n", "history"}, {"esc", "leave editor"}}
	}
	return []hint{{"↑↓ ←→", "scroll"}, {"tab", "editor"}, {"t", "tables"}, {"q", "quit"}}
}
