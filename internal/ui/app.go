package ui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"dbx/internal/engine"
)

type viewID int

const (
	viewTables viewID = iota
	viewDetails
	viewSQL
)

// hint is one `<key> <label>` pair in the footer.
type hint struct{ key, label string }

// confirmState is a pending (y/n) footer prompt that captures all keys.
type confirmState struct {
	prompt string
	yes    func(a *App) tea.Cmd
}

// runState tracks the single in-flight query (new queries cancel it).
type runState struct {
	token   int
	cancel  context.CancelFunc
	started time.Time
}

type tickMsg struct{}

// tickCmd drives the `● running Ns` elapsed display. A variable so tests
// can stub out the one-second sleep.
var tickCmd = func() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

// App is the root bubbletea model.
type App struct {
	eng  engine.Engine
	path string

	w, h int
	view viewID
	// lastTable remembers which table screen (tables list or details) to
	// return to when flipping back from the SQL view.
	lastTable viewID

	tv *tablesView
	dv *detailsView
	sv *sqlView

	confirm *confirmState
	qtoken  int
	run     *runState
}

// New builds the root model for an opened database.
func New(eng engine.Engine, path string) *App {
	a := &App{eng: eng, path: path}
	a.tv = newTablesView(a)
	a.dv = newDetailsView(a)
	a.sv = newSQLView(a)
	return a
}

func (a *App) Init() tea.Cmd {
	return a.tv.load()
}

// startQuery cancels any in-flight query and starts fn with a fresh
// context. fn must embed the given token in the message it returns;
// finishQuery drops stale tokens.
func (a *App) startQuery(fn func(ctx context.Context, token int) tea.Msg) tea.Cmd {
	a.cancelQuery()
	ctx, cancel := context.WithCancel(context.Background())
	a.qtoken++
	token := a.qtoken
	a.run = &runState{token: token, cancel: cancel, started: time.Now()}
	return tea.Batch(func() tea.Msg { return fn(ctx, token) }, tickCmd())
}

// finishQuery reports whether token belongs to the current query and, if
// so, clears the running state.
func (a *App) finishQuery(token int) bool {
	if token != a.qtoken {
		return false
	}
	if a.run != nil && a.run.token == token {
		a.run = nil
	}
	return true
}

func (a *App) cancelQuery() {
	if a.run != nil {
		a.run.cancel()
		a.run = nil
	}
}

func (a *App) openDetails(table engine.Table) tea.Cmd {
	a.view = viewDetails
	return a.dv.open(table)
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.w, a.h = msg.Width, msg.Height
		a.layout()
		return a, nil

	case tickMsg:
		if a.run != nil {
			return a, tickCmd()
		}
		return a, nil

	case tablesLoadedMsg:
		return a, a.tv.onTablesLoaded(msg)
	case tableInfoMsg:
		a.tv.onTableInfo(msg)
		return a, nil
	case detailsLoadedMsg:
		a.dv.onLoaded(msg)
		return a, nil
	case valuesLoadedMsg:
		a.dv.onValuesLoaded(msg)
		return a, nil
	case sqlResultMsg:
		a.sv.onResult(msg)
		return a, nil

	case tea.KeyMsg:
		return a.handleKey(msg)
	}
	return a, nil
}

func (a *App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// A confirmation captures every key.
	if a.confirm != nil {
		switch key {
		case "y", "enter":
			c := a.confirm
			a.confirm = nil
			return a, c.yes(a)
		case "n", "esc", "q":
			a.confirm = nil
		}
		return a, nil
	}

	if key == "ctrl+c" { // immediate, no confirmation
		a.cancelQuery()
		a.eng.Close()
		return a, tea.Quit
	}

	// Text-input modes see keys first (esc leaves the mode).
	if a.currentTyping() {
		return a, a.currentView().typingKey(msg)
	}

	switch key {
	case "q":
		return a.quit()
	case "s":
		if a.view != viewSQL {
			a.lastTable = a.view
			a.view = viewSQL
			a.sv.focusEditor()
		}
		return a, nil
	case "t":
		if a.view == viewSQL {
			a.view = a.lastTable
		}
		return a, nil
	case "esc":
		if a.run != nil {
			a.cancelQuery()
			return a, nil
		}
		return a, a.currentView().escKey()
	}
	return a, a.currentView().key(msg)
}

// quit always confirms (ctrl+c stays the immediate escape hatch).
func (a *App) quit() (tea.Model, tea.Cmd) {
	prompt := "quit?"
	if a.run != nil {
		prompt = "a query is still running, quit anyway?"
	}
	a.confirm = &confirmState{
		prompt: prompt,
		yes: func(a *App) tea.Cmd {
			a.cancelQuery()
			a.eng.Close()
			return tea.Quit
		},
	}
	return a, nil
}

// view plumbing ------------------------------------------------------------

type viewModel interface {
	key(msg tea.KeyMsg) tea.Cmd       // normal-mode key
	typingKey(msg tea.KeyMsg) tea.Cmd // key while a text input is active
	escKey() tea.Cmd                  // esc outside typing / running query
	typing() bool
	render(w, h int) []string
	status() string // styled footer status content
	hints() []hint
}

func (a *App) currentView() viewModel {
	switch a.view {
	case viewDetails:
		return a.dv
	case viewSQL:
		return a.sv
	default:
		return a.tv
	}
}

func (a *App) currentTyping() bool { return a.currentView().typing() }

func (a *App) inner() int { return max(24, a.w-2) }

// layout pushes new sizes into the text components.
func (a *App) layout() {
	a.tv.layout(a.inner())
	a.dv.layout(a.inner())
	a.sv.layout(a.inner(), a.bodyHeight())
}

func (a *App) bodyHeight() int { return max(0, a.h-6) }

// rendering ----------------------------------------------------------------

func (a *App) View() string {
	if a.w == 0 || a.h == 0 {
		return ""
	}
	inner := a.inner()
	lines := make([]string, 0, a.h)
	lines = append(lines, a.banner(inner)...)

	bodyH := a.bodyHeight()
	body := a.currentView().render(inner, bodyH)
	if len(body) > bodyH {
		body = body[:bodyH]
	}
	lines = append(lines, body...)
	for len(lines) < a.h-2 {
		lines = append(lines, "")
	}
	lines = append(lines, a.footer(inner)...)

	// The global one-column left margin.
	for i, l := range lines {
		lines[i] = " " + l
	}
	return strings.Join(lines, "\n")
}

func (a *App) banner(inner int) []string {
	ctx := displayPath(a.path) + " · " + string(a.eng.Kind())
	if a.view == viewDetails && a.dv.hasTable {
		ctx = displayPath(a.path) + " · " + a.dv.table.Display()
	}
	return []string{
		"",
		S.Brand.Render(truncTail(glyphBar+" dbx. Data file viewer.", inner)),
		"  " + S.Dim.Render(truncMiddle(ctx, inner-2)),
		"",
	}
}

func (a *App) footer(inner int) []string {
	status := ""
	switch {
	case a.confirm != nil:
		status = S.Fail.Render(glyphBar+" ") + S.Title.Render(a.confirm.prompt+" (y/n)")
	case a.run != nil:
		secs := int(time.Since(a.run.started).Seconds())
		status = S.Accent.Render(fmt.Sprintf("%s running %ds", glyphRunning, secs))
	default:
		status = a.currentView().status()
	}
	if lw := len(status); lw > 0 {
		status = truncTailANSI(status, inner)
	}

	return []string{status, renderHints(a.currentHints(), inner)}
}

func (a *App) currentHints() []hint {
	if a.confirm != nil {
		return []hint{{"y", "confirm"}, {"n", "cancel"}}
	}
	hints := a.currentView().hints()
	if a.run != nil {
		hints = append([]hint{{"esc", "cancel query"}}, hints...)
	}
	return hints
}

// renderHints renders `<key> <label>` pairs, three spaces apart. On narrow
// widths the labels are dropped and only the keys remain.
func renderHints(hints []hint, inner int) string {
	full := make([]string, len(hints))
	plainW := 0
	for i, h := range hints {
		full[i] = S.Key.Render(h.key) + " " + S.Dim.Render(h.label)
		plainW += dw(h.key) + 1 + dw(h.label)
	}
	plainW += 3 * max(0, len(hints)-1)
	if plainW <= inner {
		return strings.Join(full, "   ")
	}
	keys := make([]string, len(hints))
	for i, h := range hints {
		keys[i] = S.Key.Render(h.key)
	}
	return strings.Join(keys, "   ")
}

// truncTailANSI clips a styled string to w cells; used only for the status
// line where overflow is rare.
func truncTailANSI(s string, w int) string {
	if lw := dw(stripToWidth(s)); lw <= w {
		return s
	}
	return truncTail(stripToWidth(s), w)
}

func displayPath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if strings.HasPrefix(p, home) {
			return "~" + strings.TrimPrefix(p, home)
		}
	}
	return p
}

// queryStatus renders the shared success/failure status for a finished
// query: `✓ 87 rows · 12ms · truncated to 100` or `✗ <error>`.
func queryStatus(rows int, elapsed time.Duration, truncated bool, err error) string {
	if err != nil {
		return S.Fail.Render(glyphFail + " " + oneLine(err.Error()))
	}
	s := S.OK.Render(glyphOK) + S.Text.Render(fmt.Sprintf(" %d rows", rows)) +
		S.Dim.Render(" · "+formatElapsed(elapsed))
	if truncated {
		s += S.Dim.Render(" · truncated to 100")
	}
	return s
}

func formatElapsed(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return fmt.Sprintf("%dµs", d.Microseconds())
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	default:
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.Join(strings.Fields(s), " ")
}
